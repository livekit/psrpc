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
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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

// holdSecondObserver holds the handler's run loop on the second request it
// receives, before the loop hands that request off, until released.
type holdSecondObserver struct {
	received atomic.Int32
	held     chan struct{}
	release  chan struct{}
}

func (o *holdSecondObserver) OnRequestReceived(psrpc.RPCInfo) {
	if o.received.Add(1) == 2 {
		close(o.held)
		<-o.release
	}
}

func (o *holdSecondObserver) OnRequestExpired(psrpc.RPCInfo, time.Duration)            {}
func (o *holdSecondObserver) OnClaim(psrpc.RPCInfo, psrpc.ClaimOutcome, time.Duration) {}

// A server closing gracefully waits for every request its handler took before
// the close began, though the loop hands one off only after the close began
// and after the requests in flight had finished. Counting that request does not
// race the close's wait for it.
func TestGracefulCloseWaitsForRequestsTakenBeforeIt(t *testing.T) {
	const rpc = "taken"

	obs := &holdSecondObserver{held: make(chan struct{}), release: make(chan struct{})}
	b := localbus.New()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b,
		psrpc.WithServerObserver(obs))
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	s.RegisterMethod(rpc, false, false, true, true)
	c.RegisterMethod(rpc, false, false, true, true)

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	var secondHandled atomic.Bool
	require.NoError(t, server.RegisterHandler(s, rpc, nil,
		func(context.Context, *internal.Request) (*internal.Response, error) {
			if calls.Add(1) == 1 {
				close(firstStarted)
				<-releaseFirst
			} else {
				secondHandled.Store(true)
			}
			return &internal.Response{}, nil
		}, nil))

	request := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = client.RequestSingle[*internal.Response](ctx, c, rpc, nil, &internal.Request{})
	}
	go request()
	<-firstStarted
	go request()
	<-obs.held

	closed := make(chan struct{})
	go func() {
		s.Close(false)
		close(closed)
	}()
	// let the close reach its wait while the first request is in flight
	time.Sleep(50 * time.Millisecond)

	close(releaseFirst)
	assert.Never(t, func() bool {
		select {
		case <-closed:
			return true
		default:
			return false
		}
	}, 100*time.Millisecond, 5*time.Millisecond, "the close returned before a request taken before it was handled")

	close(obs.release)
	<-closed
	require.True(t, secondHandled.Load())
}
