// Copyright 2026 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus/bustest"
	_ "github.com/livekit/psrpc/pkg/bus/natsbus/natstest"
	_ "github.com/livekit/psrpc/pkg/bus/redisbus/redistest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

// Two servers that share one bus and register the same queue rpc are two
// members of its queue: each request must run on exactly one of them.
// Reproduction and test by @da1suk8 in livekit/psrpc#175.
func TestQueueRunsOnceWithTwoServersOnOneBus(t *testing.T) {
	bustest.TestAll(t, func(t *testing.T, newBus bustest.Connect) {
		const rpc = "queued"
		const requests = 10

		b := newBus(t)

		var calls atomic.Int32
		for range 2 {
			s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
			t.Cleanup(func() { s.Close(true) })
			s.RegisterMethod(rpc, false, false, true, true)
			require.NoError(t, server.RegisterHandler(s, rpc, nil,
				func(context.Context, *internal.Request) (*internal.Response, error) {
					calls.Add(1)
					return &internal.Response{}, nil
				}, nil))
		}

		c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
		require.NoError(t, err)
		t.Cleanup(func() { c.Close() })
		c.RegisterMethod(rpc, false, false, true, true)

		// The redis bus reconciles subscriptions asynchronously; publishing now races.
		time.Sleep(time.Second)

		for range requests {
			_, err := client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
			require.NoError(t, err)
		}

		// let a second handler, if one ran, finish
		time.Sleep(200 * time.Millisecond)
		require.EqualValues(t, requests, calls.Load(), "each queue request must run exactly once")
	})
}
