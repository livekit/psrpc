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

package bus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestNATSSubscriptionReadCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := &natsSubscription{ctx: ctx, msgChan: make(chan *nats.Msg)}
		returned := make(chan bool, 1)
		go func() {
			_, ok := s.read()
			returned <- ok
		}()
		synctest.Wait() // The reader is blocked before cancellation.
		cancel()
		synctest.Wait()
		select {
		case ok := <-returned:
			require.False(t, ok)
		default:
			// Release the old implementation's reader before failing.
			close(s.msgChan)
			<-returned
			t.Fatal("read remained blocked after cancellation")
		}
	})
}

func TestNATSSubscriptionWriteCancellation(t *testing.T) {
	for _, size := range []int{0, 1} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				s := &natsSubscription{ctx: ctx, msgChan: make(chan *nats.Msg, size)}
				if size > 0 {
					s.msgChan <- &nats.Msg{}
				}
				done := make(chan struct{})
				go func() {
					s.write(&nats.Msg{})
					close(done)
				}()
				synctest.Wait() // The callback is blocked on the full channel.
				cancel()
				synctest.Wait()
				select {
				case <-done:
				default:
					t.Fatal("write remained blocked after cancellation")
				}
			})
		})
	}
}

func TestNATSSubscriptionClose(t *testing.T) {
	ctx := context.Background()
	pool, err := dockertest.NewPool(ctx, "")
	require.NoError(t, err)
	server, err := pool.Run(ctx, "nats", dockertest.WithTag("latest"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close(ctx)) })
	var nc *nats.Conn
	require.NoError(t, pool.Retry(ctx, 0, func() error {
		var err error
		nc, err = nats.Connect("nats://"+server.GetHostPort("4222/tcp"), nats.NoReconnect())
		return err
	}))
	t.Cleanup(nc.Close)

	for _, tc := range []struct {
		name string
		size int
		full bool
	}{
		{name: "unbuffered"},
		{name: "buffered", size: 1},
		{name: "full", size: 1, full: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			s := &natsSubscription{ctx: ctx, cancel: cancel, msgChan: make(chan *nats.Msg, tc.size)}
			if tc.full {
				s.msgChan <- &nats.Msg{}
			}
			entered := make(chan struct{})
			resume := make(chan struct{})
			exited := make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			write := false // Published to the callback by closing resume.
			var err error
			s.sub, err = nc.Subscribe(tc.name, func(m *nats.Msg) {
				close(entered)
				<-resume
				if write {
					s.write(m)
				}
			})
			require.NoError(t, err)
			s.sub.SetClosedHandler(func(string) { close(exited) })
			t.Cleanup(func() {
				cancel()
				_ = s.sub.Unsubscribe()
				release()
				select {
				case <-exited:
				case <-time.After(5 * time.Second):
					t.Error("NATS callback did not exit")
				}
			})
			require.NoError(t, nc.Publish(tc.name, []byte("late")))
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("NATS callback did not start")
			}

			closed := make(chan error, 1)
			go func() { closed <- s.Close() }()
			select {
			case err := <-closed:
				require.NoError(t, err)
			case <-time.After(5 * time.Second):
				t.Fatal("Close waited for the paused callback")
			}
			require.False(t, s.sub.IsValid())

			// NATS has entered its callback but has not called write yet.
			// Assert the ownership invariant directly: choosing between a
			// canceled context and a closed-channel send would be random.
			if tc.full {
				<-s.msgChan
			}
			select {
			case _, ok := <-s.msgChan:
				require.True(t, ok, "Close closed the channel while a NATS callback can still write")
				t.Fatal("unexpected message from the paused callback")
			default:
			}
			if tc.full {
				s.msgChan <- &nats.Msg{}
			}
			write = true
			release()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("late callback did not exit after Close")
			}
		})
	}

	t.Run("typed reader", func(t *testing.T) {
		raw, err := NewNatsMessageBus(nc).Subscribe(ctx, Channel{Server: "typed"}, 0)
		require.NoError(t, err)
		s := newSubscription[*internal.Request](raw, 0, 0)
		closed := make(chan error, 1)
		go func() { closed <- s.Close() }()
		select {
		case err := <-closed:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("typed Close did not release the raw reader")
		}
		select {
		case _, ok := <-s.Channel():
			require.False(t, ok)
		default:
			t.Fatal("typed output was not closed before Close returned")
		}
	})

	t.Run("churn", func(t *testing.T) {
		b := NewNatsMessageBus(nc).(*natsMessageBus)
		for i := range 100 {
			subject := fmt.Sprintf("churn.%d", i)
			s, err := b.subscribe(ctx, subject, i%2, i%4 < 2)
			require.NoError(t, err)
			exited := make(chan struct{})
			s.sub.SetClosedHandler(func(string) { close(exited) })
			for range 8 {
				require.NoError(t, nc.Publish(subject, []byte("message")))
			}
			require.NoError(t, nc.FlushTimeout(5*time.Second))
			require.NoError(t, s.Close())
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Fatal("NATS callback did not exit during subscription churn")
			}
		}
	})
}
