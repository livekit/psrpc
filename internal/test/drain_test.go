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

package test

import (
	"context"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus/localbus"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

func cpuTime(t *testing.T) time.Duration {
	var ru syscall.Rusage
	require.NoError(t, syscall.Getrusage(syscall.RUSAGE_SELF, &ru))
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// A server closing gracefully waits for an in-flight request without burning a core.
func TestGracefulCloseWhileHandlingDoesNotSpin(t *testing.T) {
	const rpc = "drain"

	b := localbus.New()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, true, true)
	c.RegisterMethod(rpc, false, false, true, true)

	started := make(chan struct{})
	release := make(chan struct{})
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			close(started)
			<-release
			return &internal.Response{}, nil
		}, nil))

	go func() {
		_, _ = client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
	}()
	<-started

	closed := make(chan struct{})
	go func() {
		s.Close(false)
		close(closed)
	}()

	// Let the drain begin, then measure it.
	time.Sleep(50 * time.Millisecond)
	before := cpuTime(t)
	time.Sleep(500 * time.Millisecond)
	used := cpuTime(t) - before

	close(release)
	<-closed

	require.Less(t, used, 250*time.Millisecond, "the handler loop spun while draining")
}

// holdingObserver holds the handler loop inside OnRequestReceived for the n-th
// request until released. The scheduler can park the loop at exactly this
// point on its own; the observer only makes that interleaving reproducible.
type holdingObserver struct {
	mu       sync.Mutex
	received int
	holdAt   int
	held     chan struct{}
	release  chan struct{}
}

func (o *holdingObserver) OnRequestReceived(psrpc.RPCInfo) {
	o.mu.Lock()
	o.received++
	n := o.received
	o.mu.Unlock()
	if n == o.holdAt {
		close(o.held)
		<-o.release
	}
}

func (o *holdingObserver) OnRequestExpired(psrpc.RPCInfo, time.Duration)            {}
func (o *holdingObserver) OnClaim(psrpc.RPCInfo, psrpc.ClaimOutcome, time.Duration) {}

// A request the handler loop had already read off its subscription when
// Close(false) started is dispatched after Close has begun waiting. A graceful
// close must count that request too: it must not return before it has run.
func TestGracefulCloseWaitsForBufferedRequest(t *testing.T) {
	const rpc = "drain"

	obs := &holdingObserver{holdAt: 2, held: make(chan struct{}), release: make(chan struct{})}

	b := localbus.New()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs))
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, false, true)
	c.RegisterMethod(rpc, false, false, false, true)

	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondHandled := make(chan time.Time, 1)
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(_ context.Context, req *internal.Request) (*internal.Response, error) {
			switch req.RequestId {
			case "first":
				close(firstStarted)
				<-firstRelease
			case "second":
				secondHandled <- time.Now()
			}
			return &internal.Response{}, nil
		}, nil))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	send := func(id string) {
		go func() {
			_, _ = client.RequestSingle[*internal.Response](ctx, c, rpc, nil, &internal.Request{RequestId: id})
		}()
	}

	send("first")
	<-firstStarted

	// The second request is read off the subscription, but the loop is held
	// before it dispatches it.
	send("second")
	<-obs.held

	// Release the loop from a timer, so no goroutine of the test orders the
	// dispatch of the second request after Close.
	time.AfterFunc(300*time.Millisecond, func() { close(obs.release) })

	closed := make(chan time.Time, 1)
	go func() {
		s.Close(false)
		closed <- time.Now()
	}()

	time.Sleep(100 * time.Millisecond)
	close(firstRelease)

	var closedAt time.Time
	select {
	case closedAt = <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close(false) did not return")
	}

	select {
	case handledAt := <-secondHandled:
		require.False(t, handledAt.After(closedAt),
			"the second request ran %v after Close(false) returned", handledAt.Sub(closedAt))
	case <-time.After(time.Second):
		t.Fatal("the second request was read off the subscription but never handled")
	}
}
