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
