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
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

// A NATS callback that is already running when Close returns still calls write.
func TestSubscriptionWriteAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := &subscription{
		ctx:     ctx,
		cancel:  cancel,
		sub:     &nats.Subscription{},
		msgChan: make(chan *nats.Msg, 1),
	}
	_ = sub.Close()

	require.NotPanics(t, func() {
		for range 100 {
			sub.write(&nats.Msg{Data: []byte("late")})
		}
	})

	// Read may still return what is buffered, then reports the close
	buffered := 0
	for {
		if _, ok := sub.Read(); !ok {
			break
		}
		buffered++
	}
	require.LessOrEqual(t, buffered, cap(sub.msgChan))
}

func newTestRouter() *router {
	t := &transport{routers: map[string]*router{}}
	r := &router{
		sub:     &nats.Subscription{},
		routes:  map[string][]*routerSubscription{},
		t:       t,
		channel: "SRV.test",
	}
	t.routers[r.channel] = r
	return r
}

func newTestRouterSubscription(r *router, channel string, size int) *routerSubscription {
	ctx, cancel := context.WithCancel(context.Background())
	s := &routerSubscription{
		ctx:     ctx,
		cancel:  cancel,
		msgChan: make(chan *nats.Msg, size),
		router:  r,
		channel: channel,
	}
	r.open(channel, s)
	return s
}

// A routed subscriber that stops draining must not block other subscribers
// on its router, or subscription changes anywhere on the transport.
func TestRouterSlowSubscriber(t *testing.T) {
	r := newTestRouter()
	slow := newTestRouterSubscription(r, "c", 1)
	other := newTestRouterSubscription(r, "c", 1)
	t.Cleanup(slow.cancel)

	b, err := proto.Marshal(&internal.Msg{Channel: "c"})
	require.NoError(t, err)
	m := &nats.Msg{Data: b}
	r.write(m)
	done := make(chan struct{})
	go func() {
		r.write(m) // blocks: slow's buffer is full and nobody reads it
		close(done)
	}()

	// Give the write time to block; it must not be holding r.mu when it does.
	time.Sleep(100 * time.Millisecond)

	closed := make(chan struct{})
	go func() {
		_ = other.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close of another subscriber blocked behind the slow one")
	}

	slow.cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("write did not finish after the slow subscriber was cancelled")
	}
}

// A delivery still in flight when Close runs must not panic.
func TestRouterSubscriptionWriteAfterClose(t *testing.T) {
	r := newTestRouter()
	s := newTestRouterSubscription(r, "c", 1)
	_ = s.Close()

	require.NotPanics(t, func() {
		for range 100 {
			s.write(&nats.Msg{})
		}
	})
}

// Read returns what was buffered before Close, then reports the close.
func TestRouterSubscriptionReadDrainsAfterClose(t *testing.T) {
	r := newTestRouter()
	s := newTestRouterSubscription(r, "c", 3)
	for range 3 {
		s.write(&nats.Msg{})
	}
	_ = s.Close()

	for range 3 {
		_, ok := s.Read()
		require.True(t, ok)
	}
	_, ok := s.Read()
	require.False(t, ok)
}
