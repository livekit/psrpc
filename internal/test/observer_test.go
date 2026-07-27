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
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

type recordingObserver struct {
	mu       sync.Mutex
	received int
	claims   []psrpc.ClaimOutcome
}

func (o *recordingObserver) OnRequestReceived(psrpc.RPCInfo) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.received++
}

func (o *recordingObserver) OnRequestExpired(psrpc.RPCInfo, time.Duration) {}

func (o *recordingObserver) OnClaim(_ psrpc.RPCInfo, outcome psrpc.ClaimOutcome, _ time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.claims = append(o.claims, outcome)
}

func (o *recordingObserver) snapshot() (received int, claims []psrpc.ClaimOutcome) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.received, append([]psrpc.ClaimOutcome(nil), o.claims...)
}

// The timed-out leg relies on a slow affinity function to delay the bid past
// WithClientSelectTimeout, so the claim expires ungranted while the request
// itself was delivered.
func TestRequestObserver(t *testing.T) {
	obs := &recordingObserver{}
	b := bus.NewLocalMessageBus()

	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithClientSelectTimeout(20*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	const granted, timedOut = "observed_granted", "observed_timed_out"
	for _, rpc := range []string{granted, timedOut} {
		// affinityEnabled is set for the timed-out RPC only, so its affinity function
		// runs before the bid.
		affinity := rpc == timedOut
		s.RegisterMethod(rpc, affinity, false, true, false)
		c.RegisterMethod(rpc, affinity, false, true, false)
	}

	require.NoError(t, server.RegisterHandler(s, granted, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			return &internal.Response{}, nil
		}, nil))

	handlerRan := make(chan struct{}, 1)
	require.NoError(t, server.RegisterHandler(s, timedOut, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			handlerRan <- struct{}{}
			return &internal.Response{}, nil
		},
		func(context.Context, *internal.Request) float32 {
			// Bid later than the client is willing to wait.
			time.Sleep(60 * time.Millisecond)
			return 1
		}))

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, granted, nil, &internal.Request{})
	require.NoError(t, err)

	_, err = client.RequestSingle[*internal.Response](context.Background(), c, timedOut, nil,
		&internal.Request{}, psrpc.WithRequestTimeout(300*time.Millisecond))
	require.ErrorIs(t, err, psrpc.ErrNoResponse, "client must give up before the bid lands")

	// The timed-out claim settles at request expiry, after the client has
	// already given up.
	require.Eventually(t, func() bool {
		_, claims := obs.snapshot()
		return len(claims) == 2
	}, 2*time.Second, 10*time.Millisecond, "both claim outcomes must be observed")

	received, claims := obs.snapshot()
	require.Equal(t, 2, received, "both requests were delivered")
	require.Equal(t, []psrpc.ClaimOutcome{psrpc.ClaimGranted, psrpc.ClaimTimedOut}, claims)

	// Claim never granted => handler must not run.
	select {
	case <-handlerRan:
		t.Fatal("handler ran despite the claim never being granted")
	default:
	}
}
