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

package bus_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/localbus"
)

func TestSubscriptionIgnoresUnexpectedMessageType(t *testing.T) {
	b := localbus.New()
	channel := bus.Channel{Legacy: "test"}
	sub, err := bus.Subscribe[*internal.Request](context.Background(), b, channel, 1)
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
	b := localbus.New()
	sub, err := bus.Subscribe[*internal.Request](
		context.Background(), b, bus.Channel{Legacy: "test"}, 1,
	)
	require.NoError(t, err)

	require.NoError(t, sub.Close())
	require.NoError(t, sub.Close())

	select {
	case _, ok := <-sub.Channel():
		require.False(t, ok)
	default:
		t.Fatal("subscription channel was not closed before Close returned")
	}
}
