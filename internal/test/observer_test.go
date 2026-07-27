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
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/middleware"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

type recordingObserver struct {
	mu       sync.Mutex
	received int
	expired  int
	claims   []psrpc.ClaimOutcome
	waits    []time.Duration
}

func (o *recordingObserver) OnRequestReceived(psrpc.RPCInfo) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.received++
}

func (o *recordingObserver) OnRequestExpired(_ psrpc.RPCInfo, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expired++
}

func (o *recordingObserver) OnClaim(_ psrpc.RPCInfo, outcome psrpc.ClaimOutcome, wait time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.claims = append(o.claims, outcome)
	o.waits = append(o.waits, wait)
}

func (o *recordingObserver) snapshot() (received, expired int, claims []psrpc.ClaimOutcome) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.received, o.expired, append([]psrpc.ClaimOutcome(nil), o.claims...)
}

func TestRequestObserverClaimGranted(t *testing.T) {
	obs := &recordingObserver{}
	rpc := "observed_ok"
	svc := &info.ServiceDefinition{Name: "test", ID: rand.NewString()}
	b := bus.NewLocalMessageBus()

	s := server.NewRPCServer(svc, b, psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, true, false)
	c.RegisterMethod(rpc, false, false, true, false)
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
	require.NoError(t, err)

	received, expired, claims := obs.snapshot()
	require.Equal(t, 1, received, "delivery must be observed")
	require.Equal(t, 0, expired)
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted}, claims)
}

// Slow affinity delays the bid past WithClientSelectTimeout, so the claim
// expires ungranted while the request itself was delivered.
func TestRequestObserverClaimAbandoned(t *testing.T) {
	obs := &recordingObserver{}
	rpc := "observed_abandoned"
	svc := &info.ServiceDefinition{Name: "test", ID: rand.NewString()}
	b := bus.NewLocalMessageBus()

	s := server.NewRPCServer(svc, b, psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithClientSelectTimeout(20*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	// affinityEnabled=true so the affinity function runs before the bid.
	s.RegisterMethod(rpc, true, false, true, false)
	c.RegisterMethod(rpc, true, false, true, false)

	handlerRan := make(chan struct{}, 1)
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			handlerRan <- struct{}{}
			return &internal.Response{}, nil
		},
		func(context.Context, *internal.Request) float32 {
			// Bid later than the client is willing to wait.
			time.Sleep(60 * time.Millisecond)
			return 1
		}))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil,
		&internal.Request{}, psrpc.WithRequestTimeout(300*time.Millisecond))
	require.ErrorIs(t, err, psrpc.ErrNoResponse, "client must give up before the bid lands")

	// Claim settles at request expiry, after the client has already given up.
	require.Eventually(t, func() bool {
		_, _, claims := obs.snapshot()
		return len(claims) == 1
	}, 2*time.Second, 10*time.Millisecond, "claim outcome must be observed")

	received, expired, claims := obs.snapshot()
	require.Equal(t, 1, received, "request was delivered")
	require.Equal(t, 0, expired)
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimAbandoned}, claims)

	// Claim never granted => handler must not run.
	select {
	case <-handlerRan:
		t.Fatal("handler ran despite the claim never being granted")
	default:
	}
}

// noopMetrics is a MetricsObserver that records nothing.
type noopMetrics struct{}

func (noopMetrics) OnUnaryRequest(middleware.MetricRole, psrpc.RPCInfo, time.Duration, error, int, int) {
}
func (noopMetrics) OnMultiRequest(middleware.MetricRole, psrpc.RPCInfo, time.Duration, int, int, int, int) {
}
func (noopMetrics) OnStreamSend(middleware.MetricRole, psrpc.RPCInfo, time.Duration, error, int) {}
func (noopMetrics) OnStreamRecv(middleware.MetricRole, psrpc.RPCInfo, error, int)                {}
func (noopMetrics) OnStreamOpen(middleware.MetricRole, psrpc.RPCInfo)                            {}
func (noopMetrics) OnStreamClose(middleware.MetricRole, psrpc.RPCInfo)                           {}

type metricsAndRequestObserver struct {
	*recordingObserver
	noopMetrics
}

func TestWithServerMetricsWiresRequestObserver(t *testing.T) {
	rec := &recordingObserver{}
	obs := metricsAndRequestObserver{recordingObserver: rec}
	rpc := "observed_via_metrics"
	b := bus.NewLocalMessageBus()

	// Note: WithServerMetrics only -- no WithServerObserver.
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		middleware.WithServerMetrics(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, true, false)
	c.RegisterMethod(rpc, false, false, true, false)
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
	require.NoError(t, err)

	received, _, claims := rec.snapshot()
	require.Equal(t, 1, received, "delivery must be observed without WithServerObserver")
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted}, claims)
}

func TestWithServerMetricsPlainObserver(t *testing.T) {
	rpc := "metrics_only"
	b := bus.NewLocalMessageBus()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		middleware.WithServerMetrics(noopMetrics{}))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, true, false)
	c.RegisterMethod(rpc, false, false, true, false)
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
	require.NoError(t, err)
}
