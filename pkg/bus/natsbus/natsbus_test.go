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

package natsbus

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v4"
	"github.com/stretchr/testify/require"
)

func TestNATSSubscriptionReadCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := &subscription{ctx: ctx, msgChan: make(chan *nats.Msg)}
		returned := make(chan bool, 1)
		go func() {
			_, ok := s.Read()
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
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s := &subscription{ctx: ctx, msgChan: make(chan *nats.Msg)}
		done := make(chan struct{})
		go func() {
			s.write(&nats.Msg{})
			close(done)
		}()
		synctest.Wait() // With no reader, the callback is blocked on the send.
		cancel()
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("write remained blocked after cancellation")
		}
	})
}

func TestNATSSubscriptionCloseWithInflightCallback(t *testing.T) {
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

	callbackCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Leave room for a late callback to send even after cancellation.
	s := &subscription{ctx: callbackCtx, cancel: cancel, msgChan: make(chan *nats.Msg, 1)}
	entered := make(chan struct{})
	resume := make(chan bool, 1)
	exited := make(chan struct{})
	s.sub, err = nc.Subscribe("shutdown", func(m *nats.Msg) {
		close(entered)
		if <-resume {
			s.write(m)
		}
	})
	require.NoError(t, err)
	s.sub.SetClosedHandler(func(string) { close(exited) })
	t.Cleanup(func() {
		cancel()
		_ = s.sub.Unsubscribe()
		// On failure, closing resume skips the write into a possibly closed channel.
		close(resume)
		select {
		case <-exited:
		case <-time.After(5 * time.Second):
			t.Error("NATS callback did not exit")
		}
	})

	// Pause a real callback immediately before it can write to msgChan.
	require.NoError(t, nc.Publish("shutdown", []byte("late")))
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

	// Check ownership before resuming: a closed-channel send and cancellation
	// would both be selectable, so observing only callback completion is insufficient.
	select {
	case _, ok := <-s.msgChan:
		require.True(t, ok, "Close closed the channel while a NATS callback can still write")
		t.Fatal("unexpected message from the paused callback")
	default:
	}

	resume <- true
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("late callback did not exit after Close")
	}
}
