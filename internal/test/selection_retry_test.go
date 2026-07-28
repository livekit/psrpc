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
)

const (
	retrySelectTimeout = 100 * time.Millisecond
	retryTimeout       = 3 * time.Second
)

// interceptBus drops or delays individual publishes to reproduce a request or bid
// that is lost or late in transit.
type interceptBus struct {
	bus.MessageBus
	intercept func(msg proto.Message) (delay time.Duration, drop bool)
}

func (b *interceptBus) Publish(ctx context.Context, channel bus.Channel, msg proto.Message) error {
	delay, drop := b.intercept(msg)
	switch {
	case drop:
		return nil
	case delay > 0:
		go func() {
			time.Sleep(delay)
			_ = b.MessageBus.Publish(context.Background(), channel, msg)
		}()
		return nil
	default:
		return b.MessageBus.Publish(ctx, channel, msg)
	}
}

// newRetryFixture registers one queue+claim handler, matching the registration used
// by the services that hit ErrNoResponse in production.
func newRetryFixture(
	t *testing.T,
	underlying bus.MessageBus,
	intercept func(msg proto.Message) (time.Duration, bool),
	clientOpts ...psrpc.ClientOption,
) (*client.RPCClient, string, *int32) {
	t.Helper()

	serviceName := "selection_retry"
	rpc := "create"
	b := &interceptBus{MessageBus: underlying, intercept: intercept}

	svc := server.NewRPCServer(&info.ServiceDefinition{
		Name: serviceName,
		ID:   rand.NewString(),
	}, b)
	t.Cleanup(func() { svc.Close(true) })

	var calls int32
	handler := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		atomic.AddInt32(&calls, 1)
		return &internal.Response{RequestId: req.RequestId}, nil
	}

	svc.RegisterMethod(rpc, false, false, true, true)
	err := server.RegisterHandler[*internal.Request, *internal.Response](svc, rpc, nil, handler, nil)
	require.NoError(t, err)

	opts := append([]psrpc.ClientOption{
		psrpc.WithClientTimeout(retryTimeout),
		psrpc.WithClientSelectTimeout(retrySelectTimeout),
	}, clientOpts...)

	c, err := client.NewRPCClient(&info.ServiceDefinition{
		Name: serviceName,
		ID:   rand.NewString(),
	}, b, opts...)
	require.NoError(t, err)
	c.RegisterMethod(rpc, false, false, true, true)

	return c, rpc, &calls
}

// dropNthRequest reproduces hypothesis A: the request never reaches a responder, so
// no bid is ever published and the caller's selection window closes empty.
func dropNthRequest(n int32) func(proto.Message) (time.Duration, bool) {
	var seen int32
	return func(msg proto.Message) (time.Duration, bool) {
		if _, ok := msg.(*internal.Request); ok {
			return 0, atomic.AddInt32(&seen, 1) == n
		}
		return 0, false
	}
}

func dropAllRequests(msg proto.Message) (time.Duration, bool) {
	_, ok := msg.(*internal.Request)
	return 0, ok
}

func TestSelectionRetry(t *testing.T) {
	bustest.TestAll(t, func(t *testing.T, newBus func(t testing.TB) bus.MessageBus) {
		testSelectionRetry(t, newBus)
	})
}

func testSelectionRetry(t *testing.T, newBus func(t testing.TB) bus.MessageBus) {
	// A lost request with no retry configured is the production failure: the caller
	// reads no bid and gives up at the selection deadline.
	t.Run("LostRequestFailsWithoutRetry", func(t *testing.T) {
		c, rpc, calls := newRetryFixture(t, newBus(t), dropNthRequest(1))

		_, err := client.RequestSingle[*internal.Response](
			context.Background(), c, rpc, nil, &internal.Request{RequestId: rand.NewRequestID()},
		)
		require.ErrorIs(t, err, psrpc.ErrNoResponse)
		require.Equal(t, int32(0), atomic.LoadInt32(calls))
	})

	t.Run("LostRequestRecoveredByRetry", func(t *testing.T) {
		c, rpc, calls := newRetryFixture(t, newBus(t), dropNthRequest(1),
			psrpc.WithClientSelectionAttempts(2))

		requestID := rand.NewRequestID()
		res, err := client.RequestSingle[*internal.Response](
			context.Background(), c, rpc, nil, &internal.Request{RequestId: requestID},
		)
		require.NoError(t, err)
		require.Equal(t, requestID, res.RequestId)
		require.Equal(t, int32(1), atomic.LoadInt32(calls), "republishing must not duplicate handler execution")
	})

	// A bid that arrives after the selection deadline is still usable, because the
	// retry reuses the request id and so the caller's claim channel is still registered.
	// The server that bid late receives the republished request as well, and must run
	// the handler exactly once across both deliveries.
	t.Run("LateBidRunsHandlerExactlyOnce", func(t *testing.T) {
		var bids int32
		intercept := func(msg proto.Message) (time.Duration, bool) {
			if _, ok := msg.(*internal.ClaimRequest); ok {
				if atomic.AddInt32(&bids, 1) == 1 {
					return retrySelectTimeout * 2, false
				}
			}
			return 0, false
		}

		// Enough attempts that the delayed bid lands inside a selection window on any
		// bus, rather than in the gap after the last one.
		c, rpc, calls := newRetryFixture(t, newBus(t), intercept,
			psrpc.WithClientSelectionAttempts(10))

		requestID := rand.NewRequestID()
		res, err := client.RequestSingle[*internal.Response](
			context.Background(), c, rpc, nil, &internal.Request{RequestId: requestID},
		)
		require.NoError(t, err)
		require.Equal(t, requestID, res.RequestId)
		require.Equal(t, int32(1), atomic.LoadInt32(calls))
		require.Equal(t, int32(1), atomic.LoadInt32(&bids),
			"a server must bid at most once per request id, however often the request is redelivered")
	})

	// Retries are bounded by the caller's deadline, not by the attempt count, so a
	// permanently unreachable service still fails at the deadline rather than looping.
	t.Run("RetriesBoundedByDeadline", func(t *testing.T) {
		c, rpc, calls := newRetryFixture(t, newBus(t), dropAllRequests,
			psrpc.WithClientSelectionAttempts(1000))

		start := time.Now()
		_, err := client.RequestSingle[*internal.Response](
			context.Background(), c, rpc, nil, &internal.Request{RequestId: rand.NewRequestID()},
		)
		require.Error(t, err)
		require.Less(t, time.Since(start), retryTimeout*2)
		require.Equal(t, int32(0), atomic.LoadInt32(calls))
	})
}
