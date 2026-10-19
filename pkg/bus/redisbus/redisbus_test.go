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
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/go-logr/logr"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/redisbus"
	"github.com/livekit/psrpc/pkg/bus/redisbus/redistest"
)

// The psrpc logger is a package global read without synchronization by every bus
// goroutine, including ones earlier tests leave running, so it is installed once
// before any test starts rather than swapped per test.
var testLogs = &logRecorder{}

func TestMain(m *testing.M) {
	logger.SetLogger(logr.New(testLogs))
	os.Exit(m.Run())
}

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

func TestRedisMessageBusQueueLock(t *testing.T) {
	srv := redistest.New(t)
	addr := srv.(interface{ Addr() string }).Addr()

	connectWithHook := func(t *testing.T, h *setHook) bus.MessageBus {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{addr}})
		t.Cleanup(func() { _ = rc.Close() })
		rc.AddHook(h)
		return redisbus.New(rc)
	}

	readValue := func(r bus.Reader) <-chan string {
		read := make(chan string, 1)
		go func() {
			b, ok := r.Read()
			if !ok {
				return
			}
			if msg, err := bus.Deserialize(b, 0); err == nil {
				read <- msg.(*wrapperspb.StringValue).Value
			}
		}()
		return read
	}

	t.Run("a queue lock that fails is logged", func(t *testing.T) {
		channel := fmt.Sprintf("lock-failed-%d", time.Now().UnixNano())
		h := &setHook{}
		h.fail.Store(true)
		sub := connectWithHook(t, h)
		pub := srv.Connect(t)

		r, err := sub.SubscribeQueue(context.Background(), redisTestChannel(channel), 100)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)
		read := readValue(r)

		require.NoError(t, pub.Publish(context.Background(), redisTestChannel(channel), wrapperspb.String("dropped")))
		require.Eventually(t, func() bool {
			return testLogs.count("redis queue lock failed", channel) == 1
		}, time.Second, 10*time.Millisecond)

		h.fail.Store(false)
		require.NoError(t, pub.Publish(context.Background(), redisTestChannel(channel), wrapperspb.String("delivered")))
		select {
		case v := <-read:
			require.Equal(t, "delivered", v)
		case <-time.After(time.Second):
			t.Fatal("the message after the failure was not delivered")
		}
		require.Equal(t, 1, testLogs.count("redis queue lock failed", channel))
	})

	t.Run("a queue lock another receiver won is not logged", func(t *testing.T) {
		channel := fmt.Sprintf("lock-lost-%d", time.Now().UnixNano())
		h := &setHook{}
		b1 := connectWithHook(t, h)
		b2 := connectWithHook(t, h)
		pub := srv.Connect(t)

		r1, err := b1.SubscribeQueue(context.Background(), redisTestChannel(channel), 100)
		require.NoError(t, err)
		r2, err := b2.SubscribeQueue(context.Background(), redisTestChannel(channel), 100)
		require.NoError(t, err)
		time.Sleep(100 * time.Millisecond)

		var n atomic.Int64
		for _, r := range []bus.Reader{r1, r2} {
			go func(r bus.Reader) {
				if _, ok := r.Read(); ok {
					n.Inc()
				}
			}(r)
		}

		require.NoError(t, pub.Publish(context.Background(), redisTestChannel(channel), wrapperspb.String("once")))
		// Both receivers try the lock, and only one gets it.
		require.Eventually(t, func() bool { return h.sets.Load() == 2 }, time.Second, 10*time.Millisecond)
		time.Sleep(100 * time.Millisecond)

		require.EqualValues(t, 1, n.Load())
		require.Zero(t, testLogs.count("redis queue lock failed", channel))
	})
}

// setHook counts SET commands and, while fail is set, fails them before they reach Redis.
type setHook struct {
	fail atomic.Bool
	sets atomic.Int64
}

func (h *setHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *setHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() != "set" {
			return next(ctx, cmd)
		}
		h.sets.Inc()
		if h.fail.Load() {
			err := errors.New("injected SET failure")
			cmd.SetErr(err)
			return err
		}
		return next(ctx, cmd)
	}
}

func (h *setHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// logRecorder is a logr sink that keeps every entry for tests to inspect.
type logRecorder struct {
	mu      sync.Mutex
	entries []logEntry
}

type logEntry struct {
	err error
	msg string
	kv  []any
}

func (l *logRecorder) Init(logr.RuntimeInfo) {}

func (l *logRecorder) Enabled(int) bool { return true }

func (l *logRecorder) Info(_ int, msg string, kv ...any) {
	l.record(logEntry{msg: msg, kv: kv})
}

func (l *logRecorder) Error(err error, msg string, kv ...any) {
	l.record(logEntry{err: err, msg: msg, kv: kv})
}

func (l *logRecorder) WithValues(...any) logr.LogSink { return l }

func (l *logRecorder) WithName(string) logr.LogSink { return l }

func (l *logRecorder) record(e logEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
}

// count returns how many errors were logged as msg with channel as their "channel" value.
func (l *logRecorder) count(msg, channel string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	var n int
	for _, e := range l.entries {
		if e.err == nil || e.msg != msg {
			continue
		}
		for i := 0; i+1 < len(e.kv); i += 2 {
			if e.kv[i] == "channel" && e.kv[i+1] == channel {
				n++
			}
		}
	}
	return n
}

func TestFoo(t *testing.T) {
	foo := "asdf"
	fmt.Println(*(*[]byte)(unsafe.Pointer(&foo)))
}
