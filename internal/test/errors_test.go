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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twitchtv/twirp"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus/localbus"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

func TestAs(t *testing.T) {
	err := psrpc.NewErrorf(psrpc.NotFound, "test error")

	var twErr twirp.Error
	var psrpcErr psrpc.Error

	ret := errors.As(err, &twErr)
	assert.True(t, ret)
	assert.Equal(t, twirp.NotFound, twErr.Code())

	ret = errors.As(err, &psrpcErr)
	assert.True(t, ret)
	assert.Equal(t, err, psrpcErr)
}

// An error whose message is empty is still an error: the caller got a
// success with an empty response, and acted on its zero values.
func TestErrorWithoutMessageIsNotASuccess(t *testing.T) {
	b := localbus.New()
	s := server.NewRPCServer(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	t.Cleanup(func() { s.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: "test", ID: rand.NewString()}, b)
	require.NoError(t, err)
	t.Cleanup(c.Close)

	for _, queue := range []bool{false, true} {
		rpc := fmt.Sprintf("fail_queue_%v", queue)
		s.RegisterMethod(rpc, false, false, true, queue)
		c.RegisterMethod(rpc, false, false, true, queue)
		require.NoError(t, server.RegisterHandler(s, rpc, nil,
			func(context.Context, *internal.Request) (*internal.Response, error) {
				return nil, psrpc.NewError(psrpc.Unavailable, errors.New(""))
			}, nil))

		res, err := client.RequestSingle[*internal.Response](context.Background(), c, rpc, nil, &internal.Request{})
		require.Error(t, err, "queue %v: answered %v", queue, res)
		var perr psrpc.Error
		require.True(t, errors.As(err, &perr))
		require.Equal(t, psrpc.Unavailable, perr.Code())
	}
}
