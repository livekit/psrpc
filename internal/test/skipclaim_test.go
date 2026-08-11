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

// Run on every bus: the property comes from SubscribeQueue, not any one broker.
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

		// The redis bus reconciles subscriptions asynchronously; publishing now races.
		time.Sleep(time.Second)

		_, err = client.RequestSingle[*internal.Response](context.Background(), c, queued, nil, &internal.Request{})
		require.NoError(t, err, "queue RPC must complete without a claim")

		received, claims := obs.snapshot()
		require.Equal(t, 1, received)
		require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimSkipped}, claims,
			"a skipped claim must still be observable")
		require.EqualValues(t, 1, queuedCalls.Load(), "handler must run exactly once")

		_, err = client.RequestSingle[*internal.Response](context.Background(), c, broadcast, nil, &internal.Request{})
		require.NoError(t, err)

		received, claims = obs.snapshot()
		require.Equal(t, 2, received)
		require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimSkipped, psrpc.ClaimGranted}, claims,
			"broadcast RPC must still claim")
		require.EqualValues(t, 1, broadcastCalls.Load())
	})
}

// Generated code cannot pair these, but RegisterHandler is exported.
func TestQueueRejectsAffinityFunc(t *testing.T) {
	b := bus.NewLocalMessageBus()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })

	handler := func(context.Context, *internal.Request) (*internal.Response, error) {
		return &internal.Response{}, nil
	}
	affinity := func(context.Context, *internal.Request) float32 { return 1 }

	s.RegisterMethod("queued", false, false, true, true)
	err := server.RegisterHandler(s, "queued", nil, handler, affinity)
	require.Error(t, err)
	code, ok := psrpc.GetErrorCode(err)
	require.True(t, ok)
	require.Equal(t, psrpc.InvalidArgument, code)

	s.RegisterMethod("broadcast", true, false, true, false)
	require.NoError(t, server.RegisterHandler(s, "broadcast", nil, handler, affinity))
}

// Same reasoning as the affinity function, but the caller sets these per request.
func TestQueueRejectsAffinitySelection(t *testing.T) {
	b := bus.NewLocalMessageBus()
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	c.RegisterMethod("queued", false, false, true, true)
	for _, opts := range []psrpc.SelectionOpts{
		{SelectionFunc: func([]*psrpc.Claim) (string, error) { return "", nil }},
		{MinimumAffinity: 0.5},
		{MaximumAffinity: 1},
	} {
		_, err := client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil,
			&internal.Request{}, psrpc.WithSelectionOpts(opts))
		code, ok := psrpc.GetErrorCode(err)
		require.True(t, ok)
		require.Equal(t, psrpc.InvalidArgument, code)
	}

	// AcceptFirstAvailable and AffinityTimeout are defaulted in for every method.
	c.RegisterMethod("plain", false, false, true, true)
	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "plain", nil,
		&internal.Request{}, psrpc.WithSelectionOpts(psrpc.SelectionOpts{
			AcceptFirstAvailable: true, AffinityTimeout: time.Millisecond * 50,
		}))
	require.NotErrorIs(t, err, psrpc.ErrRequestCanceled)
	code, ok := psrpc.GetErrorCode(err)
	require.True(t, ok)
	require.NotEqual(t, psrpc.InvalidArgument, code, "no handler registered, but not a config error")
}
