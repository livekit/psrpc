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
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type controlledReader struct {
	messages   chan []byte
	reads      chan struct{}
	closeCalls atomic.Int32
}

func newControlledReader(size int) *controlledReader {
	return &controlledReader{
		messages: make(chan []byte, size),
		reads:    make(chan struct{}, size),
	}
}

func (r *controlledReader) read() ([]byte, bool) {
	b, ok := <-r.messages
	if ok {
		r.reads <- struct{}{}
	}
	return b, ok
}

func (r *controlledReader) Close() error {
	if r.closeCalls.Add(1) == 1 {
		close(r.messages)
	}
	return nil
}

func TestSubscriptionIgnoresUnexpectedMessageType(t *testing.T) {
	b := NewLocalMessageBus()
	channel := Channel{Legacy: "test"}
	sub, err := Subscribe[*internal.Request](context.Background(), b, channel, 1)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sub.Close()) })

	require.NoError(t, b.Publish(context.Background(), channel, &internal.Response{}))
	want := &internal.Request{RequestId: "expected"}
	require.NoError(t, b.Publish(context.Background(), channel, want))

	select {
	case got := <-sub.Channel():
		require.True(t, proto.Equal(want, got))
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the expected message")
	}
}

func TestSubscriptionCloseIsIdempotent(t *testing.T) {
	b := NewLocalMessageBus()
	sub, err := Subscribe[*internal.Request](
		context.Background(), b, Channel{Legacy: "test"}, 1,
	)
	require.NoError(t, err)

	require.NoError(t, sub.Close())
	require.NoError(t, sub.Close())

	select {
	case _, ok := <-sub.Channel():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("subscription channel did not close")
	}
}

func TestSubscriptionCloseUnblocksBlockedDelivery(t *testing.T) {
	r := newControlledReader(2)
	sub := newSubscription[*internal.Request](r, 1, 0)

	first := &internal.Request{RequestId: "first"}
	second := &internal.Request{RequestId: "second"}
	firstBytes, err := serialize(first, "", nil)
	require.NoError(t, err)
	secondBytes, err := serialize(second, "", nil)
	require.NoError(t, err)

	r.messages <- firstBytes
	r.messages <- secondBytes
	<-r.reads
	<-r.reads

	require.NoError(t, sub.Close())

	got, ok := <-sub.Channel()
	require.True(t, ok)
	require.True(t, proto.Equal(first, got))
	select {
	case _, ok = <-sub.Channel():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("subscription channel did not close after blocked delivery was canceled")
	}
}

func TestSubscriptionConcurrentCloseCallsReaderOnce(t *testing.T) {
	r := newControlledReader(0)
	sub := newSubscription[*internal.Request](r, 1, 0)

	const callers = 16
	start := make(chan struct{})
	errs := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			errs <- sub.Close()
		}()
	}
	close(start)

	for range callers {
		require.NoError(t, <-errs)
	}
	require.Equal(t, int32(1), r.closeCalls.Load())

	select {
	case _, ok := <-sub.Channel():
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("subscription channel did not close")
	}
}
