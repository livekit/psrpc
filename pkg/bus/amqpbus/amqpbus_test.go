// Copyright 2025 LiveKit, Inc.
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

package amqpbus

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus"
)

func newTestTransport(t *testing.T) *transport {
	ctx, cancel := context.WithCancel(context.Background())
	b := &transport{
		url:     "amqp://invalid.invalid/",
		ctx:     ctx,
		cancel:  cancel,
		done:    ctx.Done(),
		subs:    map[*subscription]struct{}{},
		routers: map[string]*router{},
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// TestRouterDemux drives the router demultiplexing path without a broker:
// the router's write is fed directly, the way the underlying subscription's
// sink does. This is the data path that was broken before the sink rework
// (r.write was never called).
func TestRouterDemux(t *testing.T) {
	b := newTestTransport(t)

	r := &router{
		t:      b,
		subject: "SRV.svc",
		queue:   false,
		entries: map[string][]*subscription{},
	}
	b.mu.Lock()
	b.routers[r.subject] = r
	b.mu.Unlock()

	newReader := func(local string) *subscription {
		subCtx, cancel := context.WithCancel(b.ctx)
		s := &subscription{
			bus:    b,
			ctx:    subCtx,
			cancel: cancel,
			msgs:   make(chan []byte, 4),
			r:      r,
			local:  local,
		}
		b.mu.Lock()
		r.entries[local] = append(r.entries[local], s)
		b.mu.Unlock()
		return s
	}

	a := newReader("Method.REQ")
	c := newReader("Other.STR")

	// Feed the router the way the underlying subscription's sink does: the
	// bus envelope (internal.Msg) carrying Channel.Local, as serialize() put
	// it on the wire.
	mk := func(local, id string) []byte {
		raw, err := proto.Marshal(&internal.Request{RequestId: id})
		if err != nil {
			t.Fatal(err)
		}
		envelope, err := proto.Marshal(&internal.Msg{
			TypeUrl: "type.googleapis.com/" + string((&internal.Request{}).ProtoReflect().Descriptor().FullName()),
			Value:   raw,
			Channel: local,
		})
		if err != nil {
			t.Fatal(err)
		}
		return envelope
	}

	r.write(mk("Method.REQ", "1"))
	r.write(mk("Other.STR", "2"))

	expect := func(s *subscription, id string) {
		t.Helper()
		select {
		case envelope := <-s.msgs:
			var m internal.Msg
			if err := proto.Unmarshal(envelope, &m); err != nil {
				t.Fatalf("payload not the bus envelope: %v", err)
			}
			var req internal.Request
			if err := proto.Unmarshal(m.Value, &req); err != nil {
				t.Fatalf("envelope value not a Request: %v", err)
			}
			if req.RequestId != id {
				t.Errorf("got request %q, want %q", req.RequestId, id)
			}
		case <-time.After(time.Second):
			t.Fatalf("no message arrived for local channel of %q", id)
		}
	}
	expect(a, "1")
	expect(c, "2")

	// Unrelated Local channels must not cross into other readers.
	select {
	case payload := <-a.msgs:
		t.Errorf("reader a received an extra payload: %x", payload)
	case <-time.After(50 * time.Millisecond):
	}

	_ = a.Close()
	_ = c.Close()
	// The router must be gone once both readers closed.
	b.mu.Lock()
	_, still := b.routers[r.subject]
	b.mu.Unlock()
	if still {
		t.Error("router outlived its readers")
	}
	_ = b.Close()
}

// TestRoutedCloseWithoutPump guards the Close fast path: a routed reader
// owns no pump or done channel, so Close must return instead of waiting.
func TestRoutedCloseWithoutPump(t *testing.T) {
	b := newTestTransport(t)
	subCtx, cancel := context.WithCancel(b.ctx)
	s := &subscription{
		bus:    b,
		ctx:    subCtx,
		cancel: cancel,
		msgs:   make(chan []byte, 1),
		local:  "Method.REQ",
	}
	r := &router{
		t:      b,
		subject: "SRV.svc",
		entries: map[string][]*subscription{"Method.REQ": {s}},
	}
	s.r = r
	b.mu.Lock()
	b.routers[r.subject] = r
	b.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("routed Close blocked: it must not wait for a pump")
	}
}

var _ bus.Reader = (*subscription)(nil)
