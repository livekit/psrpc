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

package client

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
)

func Join[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
) (bus.Subscription[ResponseType], error) {
	if c.closed.IsBroken() {
		return nil, psrpc.ErrClientClosed
	}

	i := c.GetInfo(rpc, topic)
	sub, err := bus.Subscribe[ResponseType](ctx, c.bus, i.GetRPCChannel(), c.ChannelSize)
	if err != nil {
		return nil, psrpc.NewError(psrpc.Internal, err)
	}
	return sub, nil
}

func JoinQueue[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
) (bus.Subscription[ResponseType], error) {
	if c.closed.IsBroken() {
		return nil, psrpc.ErrClientClosed
	}

	i := c.GetInfo(rpc, topic)
	sub, err := bus.SubscribeQueue[ResponseType](ctx, c.bus, i.GetRPCChannel(), c.ChannelSize)
	if err != nil {
		return nil, psrpc.NewError(psrpc.Internal, err)
	}
	return sub, nil
}
