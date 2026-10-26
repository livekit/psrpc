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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus/natsbus/natstest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
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
