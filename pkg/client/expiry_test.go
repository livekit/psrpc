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

package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/localbus"
	"github.com/livekit/psrpc/pkg/info"
)

// captureRequests returns a bus that records the request messages published through it.
func captureRequests() (bus.MessageBus, <-chan *internal.Request) {
	requests := make(chan *internal.Request, 4)
	b := bus.NewTestBus(localbus.New(), func(o *bus.TestBusOpts) {
		o.PublishInterceptors = append(o.PublishInterceptors, func(next bus.PublishHandler) bus.PublishHandler {
			return func(ctx context.Context, channel bus.Channel, msg proto.Message) error {
				if req, ok := msg.(*internal.Request); ok {
					requests <- req
				}
				return next(ctx, channel, msg)
			}
		})
	})
	return b, requests
}

func TestRequestExpiryIsClampedToCallerDeadline(t *testing.T) {
	const timeout = time.Minute
	const deadline = 2 * time.Second

	testCases := []struct {
		name  string
		send  func(ctx context.Context, c *RPCClient)
		multi bool
	}{
		{
			name: "single",
			send: func(ctx context.Context, c *RPCClient) {
				_, _ = RequestSingle[*internal.Response](
					ctx, c, "rpc", nil, &internal.Request{}, psrpc.WithRequestTimeout(timeout),
				)
			},
		},
		{
			name:  "multi",
			multi: true,
			send: func(ctx context.Context, c *RPCClient) {
				_, _ = RequestMulti[*internal.Response](
					ctx, c, "rpc", nil, &internal.Request{}, psrpc.WithRequestTimeout(timeout),
				)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			b, requests := captureRequests()
			sd := &info.ServiceDefinition{Name: "test", ID: "client"}
			sd.RegisterMethod("rpc", false, tc.multi, false, false)
			c, err := NewRPCClient(sd, b)
			require.NoError(t, err)
			defer c.Close()

			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			go tc.send(ctx, c)

			select {
			case req := <-requests:
				expiry := time.Unix(0, req.Expiry)
				require.WithinDuration(t, time.Now().Add(deadline), expiry, time.Second,
					"expiry must follow the caller deadline, not the %s request timeout", timeout)
			case <-time.After(time.Second):
				t.Fatal("request was not published")
			}
		})
	}
}
