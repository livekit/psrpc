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
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus/natsbus/natstest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
	"github.com/livekit/psrpc/testutils"
)

// The broker's report of a request that nothing received is NATS behavior, so
// this runs on NATS only. The caller fails at once, instead of waiting out the
// wait the error then carries.
func TestNoSubscriberFailsFast(t *testing.T) {
	s := natstest.New(t)
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, s.Connect(t))
	require.NoError(t, err)
	t.Cleanup(func() { c.Close() })

	cases := []struct {
		rpc   string
		queue bool
		wait  time.Duration
	}{
		{"queued", true, psrpc.DefaultClientTimeout},
		{"broadcast", false, psrpc.DefaultAffinityTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.rpc, func(t *testing.T) {
			c.RegisterMethod(tc.rpc, !tc.queue, false, true, tc.queue)

			start := time.Now()
			_, err := client.RequestSingle[*internal.Response](context.Background(), c, tc.rpc, nil, &internal.Request{})
			require.Less(t, time.Since(start), psrpc.DefaultAffinityTimeout/2, "the caller must not wait")
			require.ErrorIs(t, err, psrpc.ErrNoResponse, "callers retrying ErrNoResponse must retry this")

			var nse *psrpc.NoSubscriberError
			require.True(t, errors.As(err, &nse))
			require.Equal(t, tc.wait, nse.Wait.Round(10*time.Millisecond))
		})
	}
}

// Records the channels a server answers on.
type answerRecorder struct {
	mu       sync.Mutex
	channels []string
}

func (r *answerRecorder) option() testutils.TestBusOption {
	return testutils.WithPublishInterceptor(func(next testutils.PublishHandler) testutils.PublishHandler {
		return func(ctx context.Context, channel testutils.Channel, msg proto.Message) error {
			switch msg.(type) {
			case *internal.ClaimRequest, *internal.Response:
				r.mu.Lock()
				r.channels = append(r.channels, channel.Server)
				r.mu.Unlock()
			}
			return next(ctx, channel, msg)
		}
	})
}

func (r *answerRecorder) take() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	channels := r.channels
	r.channels = nil
	return channels
}

// A caller that listens per request is answered on that request's own response
// channel, bid and response both. One that does not is answered on its shared
// channels.
func TestServerAnswersPerRequest(t *testing.T) {
	cases := []struct {
		name    string
		queue   bool
		answers int
		// A wrapped bus hides the inbox, as on a caller predating it.
		wrapClient bool
	}{
		{"queue", true, 1, false},
		{"broadcast", false, 2, false},
		{"shared channels", false, 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := natstest.New(t)
			rec := &answerRecorder{}
			srv := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()},
				testutils.NewTestBus(s.Connect(t), rec.option()))
			t.Cleanup(func() { srv.Close(true) })

			cb := s.Connect(t)
			if tc.wrapClient {
				cb = testutils.NewTestBus(cb)
			}
			c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, cb)
			require.NoError(t, err)
			t.Cleanup(func() { c.Close() })

			srv.RegisterMethod("rpc", !tc.queue, false, true, tc.queue)
			c.RegisterMethod("rpc", !tc.queue, false, true, tc.queue)
			require.NoError(t, server.RegisterHandler(srv, "rpc", nil,
				func(context.Context, *internal.Request) (*internal.Response, error) {
					return &internal.Response{}, nil
				}, nil))

			// SUB reaches the broker asynchronously, and until it does a request
			// is reported unreceived. A caller with the inbox finds out at once.
			probe, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, s.Connect(t))
			require.NoError(t, err)
			t.Cleanup(func() { probe.Close() })
			probe.RegisterMethod("rpc", !tc.queue, false, true, tc.queue)
			require.Eventually(t, func() bool {
				_, err := client.RequestSingle[*internal.Response](context.Background(), probe, "rpc", nil, &internal.Request{})
				return err == nil
			}, 2*time.Second, 10*time.Millisecond)
			rec.take()

			_, err = client.RequestSingle[*internal.Response](context.Background(), c, "rpc", nil, &internal.Request{})
			require.NoError(t, err)

			answers := rec.take()
			require.Len(t, answers, tc.answers, "a bid where one is needed, then the response")
			for _, ch := range answers {
				if tc.wrapClient {
					require.Contains(t, []string{
						info.GetClaimRequestChannel("test", c.ID).Server,
						info.GetResponseChannel("test", c.ID).Server,
					}, ch)
				} else {
					require.True(t, strings.HasPrefix(ch, "CLI.test."+c.ID+".REQ_") && strings.HasSuffix(ch, ".RES"),
						"answered on %q", ch)
				}
			}
		})
	}
}
