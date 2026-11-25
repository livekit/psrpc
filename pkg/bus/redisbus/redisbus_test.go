// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package redisbus_test

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/redisbus"
	"github.com/livekit/psrpc/pkg/bus/redisbus/redistest"
)

func redisTestChannel(channel string) bus.Channel {
	return bus.Channel{Legacy: channel}
}

func TestRedisMessageBus(t *testing.T) {
	srv := redistest.New(t)

	t.Run("published messages are received by subscribers", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)

		r, err := b0.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b1.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		b, ok := r.Read()
		require.True(t, ok)

		dst, err := bus.Deserialize(b, 0)
		require.NoError(t, err)
		require.Equal(t, src.Value, dst.(*wrapperspb.StringValue).Value)
	})

	t.Run("published messages are received by only one queue subscriber", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)
		b2 := srv.Connect(t)

		r1, err := b1.SubscribeQueue(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)
		r2, err := b2.SubscribeQueue(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		var n atomic.Int64

		go func() {
			if _, ok := r1.Read(); ok {
				n.Inc()
			}
		}()
		go func() {
			if _, ok := r2.Read(); ok {
				n.Inc()
			}
		}()

		time.Sleep(time.Second)

		require.EqualValues(t, 1, n.Load())
	})

	t.Run("closed subscriptions are unreadable", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)
		b2 := srv.Connect(t)

		r1, err := b1.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)
		r2, err := b2.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		_, ok := r1.Read()
		require.True(t, ok)
		_, ok = r2.Read()
		require.True(t, ok)

		err = r1.Close()
		require.NoError(t, err)

		time.Sleep(time.Second)

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		_, ok = r1.Read()
		require.False(t, ok)
		_, ok = r2.Read()
		require.True(t, ok)
	})
}

func TestRedisMessageBusHealthCheck(t *testing.T) {
	srv := redistest.New(t)
	addr := srv.(interface{ Addr() string }).Addr()

	const interval = 200 * time.Millisecond

	connectThrough := func(t *testing.T, p *redisProxy) bus.MessageBus {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{p.Addr()}})
		t.Cleanup(func() { _ = rc.Close() })
		return redisbus.NewWithHealthCheckInterval(rc, interval)
	}

	// Publishes want until the subscriber reads it back, skipping anything published
	// before, and returns how long that took.
	awaitDelivery := func(t *testing.T, pub bus.MessageBus, r bus.Reader, want string, within time.Duration) time.Duration {
		start := time.Now()
		received := make(chan struct{})
		go func() {
			for {
				b, ok := r.Read()
				if !ok {
					return
				}
				msg, err := bus.Deserialize(b, 0)
				if err == nil && msg.(*wrapperspb.StringValue).Value == want {
					close(received)
					return
				}
			}
		}()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		deadline := time.After(within)
		for {
			select {
			case <-received:
				return time.Since(start)
			case <-deadline:
				t.Fatalf("no message delivered within %s", within)
			case <-tick.C:
				require.NoError(t, pub.Publish(context.Background(), redisTestChannel("health"), wrapperspb.String(want)))
			}
		}
	}

	t.Run("a subscription that stops delivering is replaced", func(t *testing.T) {
		p := newRedisProxy(t, addr)
		pub := srv.Connect(t)
		sub := connectThrough(t, p)

		r, err := sub.Subscribe(context.Background(), redisTestChannel("health"), 100)
		require.NoError(t, err)
		awaitDelivery(t, pub, r, "before", time.Second)
		before := p.Accepted()

		p.SilenceExisting()

		// One interval to notice the quiet, one for the PING to go unanswered.
		elapsed := awaitDelivery(t, pub, r, "after", 10*interval)
		require.Greater(t, p.Accepted(), before, "the replacement should use a new connection")
		t.Logf("delivery resumed after %s", elapsed)
	})

	t.Run("a subscription that answers with an error is replaced", func(t *testing.T) {
		p := newRedisProxy(t, addr)
		pub := srv.Connect(t)
		sub := connectThrough(t, p)

		r, err := sub.Subscribe(context.Background(), redisTestChannel("health"), 100)
		require.NoError(t, err)
		awaitDelivery(t, pub, r, "before", time.Second)
		before := p.Accepted()

		p.InjectAndSilenceExisting([]byte("-NOAUTH Authentication required.\r\n"))

		elapsed := awaitDelivery(t, pub, r, "after", 10*interval)
		require.Greater(t, p.Accepted(), before, "the replacement should use a new connection")
		t.Logf("delivery resumed after %s", elapsed)
	})

	t.Run("a quiet subscription is kept", func(t *testing.T) {
		p := newRedisProxy(t, addr)
		pub := srv.Connect(t)
		sub := connectThrough(t, p)

		r, err := sub.Subscribe(context.Background(), redisTestChannel("health"), 100)
		require.NoError(t, err)
		awaitDelivery(t, pub, r, "before", time.Second)
		before := p.Accepted()

		time.Sleep(10 * interval)

		awaitDelivery(t, pub, r, "after", time.Second)
		require.Equal(t, before, p.Accepted(), "a healthy connection should not be replaced")
	})
}

// redisProxy forwards TCP to Redis and can make the connections it already holds go
// silent: bytes are still read from both ends but no longer forwarded, and nothing is
// closed, which is what a connection dropped without a FIN looks like to its client.
type redisProxy struct {
	ln       net.Listener
	upstream string
	accepted atomic.Int64

	mu    sync.Mutex
	conns []*redisProxyConn
}

type redisProxyConn struct {
	client, server net.Conn
	silent         atomic.Bool
	writeMu        sync.Mutex
}

func newRedisProxy(t *testing.T, upstream string) *redisProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &redisProxy{ln: ln, upstream: upstream}
	t.Cleanup(p.close)
	go p.accept()
	return p
}

func (p *redisProxy) Addr() string {
	return p.ln.Addr().String()
}

func (p *redisProxy) Accepted() int64 {
	return p.accepted.Load()
}

func (p *redisProxy) SilenceExisting() {
	p.InjectAndSilenceExisting(nil)
}

// InjectAndSilenceExisting writes frame to every held client connection, then silences it.
func (p *redisProxy) InjectAndSilenceExisting(frame []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.writeMu.Lock()
		if frame != nil {
			_, _ = c.client.Write(frame)
		}
		c.silent.Store(true)
		c.writeMu.Unlock()
	}
}

func (p *redisProxy) accept() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = client.Close()
			continue
		}
		c := &redisProxyConn{client: client, server: server}
		p.mu.Lock()
		p.conns = append(p.conns, c)
		p.mu.Unlock()
		p.accepted.Inc()
		go c.forward(c.server, c.client)
		go c.forward(c.client, c.server)
	}
}

func (c *redisProxyConn) forward(dst, src net.Conn) {
	defer func() {
		_ = src.Close()
		_ = dst.Close()
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			c.writeMu.Lock()
			var werr error
			if !c.silent.Load() {
				_, werr = dst.Write(buf[:n])
			}
			c.writeMu.Unlock()
			if werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *redisProxy) close() {
	_ = p.ln.Close()
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.client.Close()
		_ = c.server.Close()
	}
}

func BenchmarkRedisMessageBus(b *testing.B) {
	srv := redistest.New(b)

	b0 := srv.Connect(b)
	b1 := srv.Connect(b)

	r, _ := b0.Subscribe(context.Background(), redisTestChannel("test"), 100)

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			r.Read()
		}
		close(done)
	}()

	b.ResetTimer()

	src := wrapperspb.String("test")
	for i := 0; i < b.N; i++ {
		b1.Publish(context.Background(), redisTestChannel("test"), src)
	}

	<-done
}

func TestFoo(t *testing.T) {
	foo := "asdf"
	fmt.Println(*(*[]byte)(unsafe.Pointer(&foo)))
}
