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
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/bustest"
	"github.com/livekit/psrpc/pkg/bus/localbus"
	_ "github.com/livekit/psrpc/pkg/bus/natsbus/natstest"
	_ "github.com/livekit/psrpc/pkg/bus/redisbus/redistest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/middleware"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
	"github.com/livekit/psrpc/testutils"
)

// Run on every bus. A queue rpc skips the claim only on a bus that reports a
// request nothing received (NATS); on the others it is claimed as before.
func TestQueueRunsWithoutClaim(t *testing.T) {
	bustest.TestAll(t, func(t *testing.T, newBus bustest.Connect) {
		const queued, broadcast = "queued", "broadcast"

		obs := &recordingObserver{}
		b := newBus(t)
		_, skips := b.(bus.InboxBus)
		queuedClaim := psrpc.ClaimGranted
		if skips {
			queuedClaim = psrpc.ClaimSkipped
		}

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
		require.NoError(t, err)

		received, claims := obs.snapshot()
		require.Equal(t, 1, received)
		require.Equal(t, []psrpc.ClaimOutcome{queuedClaim}, claims,
			"a queue rpc skips the claim only where the bus reports unsubscribed requests")
		require.EqualValues(t, 1, queuedCalls.Load(), "handler must run exactly once")

		_, err = client.RequestSingle[*internal.Response](context.Background(), c, broadcast, nil, &internal.Request{})
		require.NoError(t, err)

		received, claims = obs.snapshot()
		require.Equal(t, 2, received)
		require.Equal(t, []psrpc.ClaimOutcome{queuedClaim, psrpc.ClaimGranted}, claims,
			"broadcast RPC must still claim")
		require.EqualValues(t, 1, broadcastCalls.Load())
	})
}

// With nothing subscribed, a queue rpc on a bus that does not report it fails
// with ErrNoResponse once the selection timeout passes, not at the request timeout.
func TestQueueNoSubscriberWithoutReports(t *testing.T) {
	const selectTimeout = 200 * time.Millisecond

	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, localbus.New(),
		psrpc.WithClientSelectTimeout(selectTimeout))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	c.RegisterMethod("queued", false, false, true, true)

	start := time.Now()
	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil,
		&internal.Request{}, psrpc.WithRequestTimeout(5*time.Second))
	require.ErrorIs(t, err, psrpc.ErrNoResponse)
	require.Less(t, time.Since(start), time.Second)
}

// Generated code cannot pair these, but RegisterHandler is exported.
func TestQueueRejectsAffinityFunc(t *testing.T) {
	b := localbus.New()
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
	b := localbus.New()
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
		&internal.Request{}, psrpc.WithRequestTimeout(time.Millisecond*50),
		psrpc.WithSelectionOpts(psrpc.SelectionOpts{
			AcceptFirstAvailable: true, AffinityTimeout: time.Millisecond * 50,
		}))
	require.NotErrorIs(t, err, psrpc.ErrRequestCanceled)
	code, ok := psrpc.GetErrorCode(err)
	require.True(t, ok)
	require.NotEqual(t, psrpc.InvalidArgument, code, "no handler registered, but not a config error")
}

// Records the grants a caller publishes.
func grantCounter(grants *atomic.Int32) testutils.TestBusOption {
	return testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
		return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
			if _, ok := msg.(*internal.ClaimResponse); ok {
				grants.Add(1)
			}
			return next(ctx, channel, msg)
		}
	})
}

// A handler slower than the selection timeout but inside the request timeout:
// the server bids before it runs the handler, so the caller grants the bid within
// the selection timeout and then waits for the response.
func TestQueueSlowHandler(t *testing.T) {
	var grants atomic.Int32
	b := testutils.NewTestBus(localbus.New(), grantCounter(&grants))

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
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
	require.EqualValues(t, 1, grants.Load(), "on a bus without no-subscriber reports the bid is granted")
}

// A queue request that arrives after the selection timeout: the caller gives up
// and retries, and the server bids on the late original too. Only the retry's
// bid is granted, so the handler runs once.
func TestQueueDelayedPastSelectionRunsOnce(t *testing.T) {
	const selectTimeout = 200 * time.Millisecond

	var published atomic.Int32
	b := delayedRequestBus(func(*internal.Request) time.Duration {
		if published.Add(1) == 1 {
			return selectTimeout * 3 / 2
		}
		return 0
	})

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithClientSelectTimeout(selectTimeout),
		middleware.WithRPCRetries(middleware.RetryOptions{MaxAttempts: 2}))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	var calls atomic.Int32
	s.RegisterMethod("queued", false, false, true, true)
	c.RegisterMethod("queued", false, false, true, true)
	require.NoError(t, server.RegisterHandler(s, "queued", nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			calls.Add(1)
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil,
		&internal.Request{}, psrpc.WithRequestTimeout(time.Second))
	require.NoError(t, err)
	require.EqualValues(t, 2, published.Load(), "the caller retries after the selection timeout")

	// let the late original arrive and bid
	time.Sleep(selectTimeout)
	require.EqualValues(t, 1, calls.Load(), "an ungranted bid must not run the handler")
}

// A caller predating no_claim still needs a bid, and gets one.
func TestQueueCallerWithoutNoClaim(t *testing.T) {
	obs := &recordingObserver{}
	b := testutils.NewTestBus(localbus.New(),
		testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
			return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
				if req, ok := msg.(*internal.Request); ok {
					req.NoClaim = false
				}
				return next(ctx, channel, msg)
			}
		}))

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
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted}, claims,
		"a caller that did not send no_claim must be bid for")
}

// A server predating no_claim bids anyway, and runs only once granted.
func TestQueueGrantsOlderServersBid(t *testing.T) {
	b := localbus.New()
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })
	c.RegisterMethod("queued", false, false, true, true)
	i := c.GetInfo("queued", nil)

	ctx := context.Background()
	requests, err := bus.SubscribeQueue[*internal.Request](ctx, b, i.GetRPCChannel(), 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = requests.Close() })
	grants, err := bus.Subscribe[*internal.ClaimResponse](ctx, b, i.GetClaimResponseChannel(), 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = grants.Close() })

	go func() {
		req := <-requests.Channel()
		if req == nil {
			return
		}
		_ = b.Publish(ctx, info.GetClaimRequestChannel("test", req.ClientId), &internal.ClaimRequest{
			RequestId: req.RequestId,
			ServerId:  "older",
			Affinity:  1,
		})
		if grant := <-grants.Channel(); grant == nil || grant.ServerId != "older" {
			return
		}
		_ = b.Publish(ctx, info.GetResponseChannel("test", req.ClientId), &internal.Response{
			RequestId: req.RequestId,
			ServerId:  "older",
		})
	}()

	_, err = client.RequestSingle[*internal.Response](ctx, c, "queued", nil, &internal.Request{},
		psrpc.WithRequestTimeout(time.Second))
	require.NoError(t, err, "the bid must be granted")
}

// Deprecated and ignored: turning it off does not bring the claim back for a
// caller that sends no_claim. The local bus does not report unsubscribed
// requests, so its caller claims; set no_claim as a NATS caller would.
func TestServerSkipClaimOptionIgnored(t *testing.T) {
	obs := &recordingObserver{}
	b := testutils.NewTestBus(localbus.New(),
		testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
			return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
				if req, ok := msg.(*internal.Request); ok {
					req.NoClaim = true
				}
				return next(ctx, channel, msg)
			}
		}))

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs), psrpc.WithServerSkipClaim(func() bool { return false }))
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
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimSkipped}, claims)
}

// A handler's error is the answer, returned without waiting out the timeout.
func TestQueueFastFailingHandler(t *testing.T) {
	b := localbus.New()

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod("queued", false, false, true, true)
	c.RegisterMethod("queued", false, false, true, true)
	require.NoError(t, server.RegisterHandler(s, "queued", nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return nil, psrpc.NewErrorf(psrpc.NotFound, "requested room does not exist")
		}, nil))

	const timeout = time.Second

	start := time.Now()
	_, err = client.RequestSingle[*internal.Response](context.Background(), c, "queued", nil,
		&internal.Request{}, psrpc.WithRequestTimeout(timeout))

	require.Error(t, err)
	code, ok := psrpc.GetErrorCode(err)
	require.True(t, ok)
	require.Equal(t, psrpc.NotFound, code, "the handler's error must reach the caller")
	require.Less(t, time.Since(start), timeout/2, "the answer must not wait out the timeout")
}

// Delivers each request matched by delay that long after it was published,
// without holding up the caller, as a backed-up subscriber would.
func delayedRequestBus(delay func(*internal.Request) time.Duration) psrpc.MessageBus {
	return testutils.NewTestBus(localbus.New(),
		testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
			return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
				if req, ok := msg.(*internal.Request); ok {
					if d := delay(req); d > 0 {
						go func() {
							time.Sleep(d)
							_ = next(ctx, channel, msg)
						}()
						return nil
					}
				}
				return next(ctx, channel, msg)
			}
		}))
}
