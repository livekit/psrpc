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

package test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/bus/bustest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

// A queue subscription selects the server before any claim exists, so the
// handshake is skipped there and kept everywhere else. Runs on every bus
// because the property comes from SubscribeQueue, not from any one broker.
func TestSkipClaim(t *testing.T) {
	bustest.TestAll(t, func(t *testing.T, newBus func(t testing.TB) bus.MessageBus) {
		const queued, broadcast = "skip_claim_queued", "skip_claim_broadcast"

		obs := &recordingObserver{}
		b := newBus(t)

		s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
			psrpc.WithServerObserver(obs))
		t.Cleanup(func() { s.Close(true) })
		c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
		require.NoError(t, err)
		t.Cleanup(func() { c.Close() })

		// Identical but for the queue flag, which is what decides the skip.
		for _, rpc := range []string{queued, broadcast} {
			queue := rpc == queued
			s.RegisterMethod(rpc, false, false, true, queue)
			c.RegisterMethod(rpc, false, false, true, queue)
		}

		var queuedCalls, broadcastCalls atomic.Int32
		require.NoError(t, server.RegisterHandler(s, queued, nil,
			func(context.Context, *internal.Request) (*internal.Response, error) {
				queuedCalls.Add(1)
				return &internal.Response{}, nil
			}, nil))
		require.NoError(t, server.RegisterHandler(s, broadcast, nil,
			func(context.Context, *internal.Request) (*internal.Response, error) {
				broadcastCalls.Add(1)
				return &internal.Response{}, nil
			}, nil))

		// The redis bus reconciles subscriptions on a write worker, so a publish
		// issued right after registration can miss them.
		time.Sleep(time.Second)

		_, err = client.RequestSingle[*internal.Response](context.Background(), c, queued, nil, &internal.Request{})
		require.NoError(t, err, "queue RPC must complete without a claim")

		received, claims := obs.snapshot()
		require.Equal(t, 1, received)
		require.Empty(t, claims, "queue RPC must not negotiate a claim")
		require.EqualValues(t, 1, queuedCalls.Load(), "handler must run exactly once")

		// The claim is still required where the bus broadcasts the request.
		_, err = client.RequestSingle[*internal.Response](context.Background(), c, broadcast, nil, &internal.Request{})
		require.NoError(t, err)

		received, claims = obs.snapshot()
		require.Equal(t, 2, received)
		require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted}, claims,
			"broadcast RPC must still claim")
		require.EqualValues(t, 1, broadcastCalls.Load())
	})
}
