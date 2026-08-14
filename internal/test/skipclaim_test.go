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
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/bus/bustest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
	"github.com/livekit/psrpc/testutils"
)

func enabled() bool { return true }

// Run on every bus: the property comes from SubscribeQueue, not any one broker.
func TestSkipClaim(t *testing.T) {
	bustest.TestAll(t, func(t *testing.T, newBus func(t testing.TB) bus.MessageBus) {
		const queued, broadcast = "skip_claim_queued", "skip_claim_broadcast"

		obs := &recordingObserver{}
		b := newBus(t)

		s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
			psrpc.WithServerObserver(obs))
		t.Cleanup(func() { s.Close(true) })
		c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
			psrpc.WithClientSkipClaim(enabled))
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

// A handler slower than the selection timeout but inside the request timeout.
// The announcement is what lets the caller tell that apart from a request
// nobody received, and the grant it replaces must not be sent.
func TestSkipClaimSlowHandler(t *testing.T) {
	var grants atomic.Int32
	b := testutils.NewTestBus(bus.NewLocalMessageBus(),
		testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
			return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
				if _, ok := msg.(*internal.ClaimResponse); ok {
					grants.Add(1)
				}
				return next(ctx, channel, msg)
			}
		}))

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithClientSkipClaim(enabled))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod("queued", false, false, true, true)
	c.RegisterMethod("queued", false, false, true, true)
	require.NoError(t, server.RegisterHandler(s, "queued", nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			// Past DefaultAffinityTimeout, inside DefaultClientTimeout.
			time.Sleep(time.Millisecond * 1500)
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil, &internal.Request{})
	require.NoError(t, err)
	require.Zero(t, grants.Load(), "an announcement needs no grant")
}

// Unset means claim, so a deploy that has not opted in is unaffected.
func TestSkipClaimDisabledByDefault(t *testing.T) {
	obs := &recordingObserver{}
	b := bus.NewLocalMessageBus()

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod("queued", false, false, true, true)
	c.RegisterMethod("queued", false, false, true, true)
	require.NoError(t, server.RegisterHandler(s, "queued", nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil, &internal.Request{})
	require.NoError(t, err)

	_, claims := obs.snapshot()
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted}, claims)
}

// The kill switch: revoking mid-flight takes effect on the next request, with no
// reconstruction of the client or server.
func TestSkipClaimRevokedAtRuntime(t *testing.T) {
	obs := &recordingObserver{}
	b := bus.NewLocalMessageBus()
	var on atomic.Bool
	on.Store(true)

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithClientSkipClaim(on.Load))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod("queued", false, false, true, true)
	c.RegisterMethod("queued", false, false, true, true)
	require.NoError(t, server.RegisterHandler(s, "queued", nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	send := func() {
		_, err := client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil, &internal.Request{})
		require.NoError(t, err)
	}

	send()
	on.Store(false)
	send()
	on.Store(true)
	send()

	_, claims := obs.snapshot()
	require.Equal(t, []psrpc.ClaimOutcome{
		psrpc.ClaimSkipped, psrpc.ClaimGranted, psrpc.ClaimSkipped,
	}, claims)
}
