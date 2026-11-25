// Copyright 2026 LiveKit, Inc.
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
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/livekit/psrpc/pkg/bus/redisbus"
	"github.com/livekit/psrpc/pkg/bus/redisbus/redistest"
)

// A subscription replaced while Redis is unreachable must receive messages
// once Redis is back. The failed UNSUBSCRIBE must not leave the channel
// marked as current, or the replacement subscription is never sent.
func TestRedisResubscribeDuringOutage(t *testing.T) {
	srv := redistest.New(t).(interface{ Addr() string })
	proxy := newToggleProxy(t, srv.Addr())

	rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{proxy.addr()}})
	t.Cleanup(func() { _ = rc.Close() })
	b := redisbus.New(rc)
	pub := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{srv.Addr()}})
	t.Cleanup(func() { _ = pub.Close() })
	pb := redisbus.New(pub)

	ctx := context.Background()
	ch := redisTestChannel("resubscribe-during-outage")

	r0, err := b.Subscribe(ctx, ch, 10)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)

	proxy.setDown(true)
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, r0.Close())
	// Let the failed UNSUBSCRIBE run before the replacement subscribes.
	time.Sleep(100 * time.Millisecond)
	r1, err := b.Subscribe(ctx, ch, 10)
	require.NoError(t, err)
	time.Sleep(2 * time.Second)
	proxy.setDown(false)

	received := make(chan struct{})
	go func() {
		if _, ok := r1.Read(); ok {
			close(received)
		}
	}()

	src := wrapperspb.String("test")
	require.Eventually(t, func() bool {
		require.NoError(t, pb.Publish(ctx, ch, src))
		select {
		case <-received:
			return true
		default:
			return false
		}
	}, 10*time.Second, 250*time.Millisecond)
}

// toggleProxy forwards TCP to upstream. While down it stops listening, so
// dials are refused as they are when Redis is restarting.
type toggleProxy struct {
	t        testing.TB
	address  string
	upstream string

	mu    sync.Mutex
	ln    net.Listener
	conns map[net.Conn]struct{}
}

func newToggleProxy(t testing.TB, upstream string) *toggleProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &toggleProxy{t: t, address: ln.Addr().String(), upstream: upstream, conns: map[net.Conn]struct{}{}}
	p.start(ln)
	t.Cleanup(func() { p.setDown(true) })
	return p
}

func (p *toggleProxy) addr() string { return p.address }

func (p *toggleProxy) start(ln net.Listener) {
	p.ln = ln
	go p.serve(ln)
}

func (p *toggleProxy) setDown(down bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if down {
		if p.ln != nil {
			_ = p.ln.Close()
			p.ln = nil
		}
		for c := range p.conns {
			_ = c.Close()
		}
		clear(p.conns)
		return
	}
	ln, err := net.Listen("tcp", p.address)
	require.NoError(p.t, err)
	p.start(ln)
}

func (p *toggleProxy) track(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range conns {
		p.conns[c] = struct{}{}
	}
}

func (p *toggleProxy) serve(ln net.Listener) {
	for {
		client, err := ln.Accept()
		if err != nil {
			return
		}
		server, err := net.Dial("tcp", p.upstream)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.track(client, server)
		go func() { _, _ = io.Copy(server, client); _ = server.Close() }()
		go func() { _, _ = io.Copy(client, server); _ = client.Close() }()
	}
}
