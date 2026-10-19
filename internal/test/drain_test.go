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
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

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
