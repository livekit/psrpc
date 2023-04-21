package client

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/channels"
)

func Join[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
) (bus.Subscription[ResponseType], error) {
	sub, err := bus.Subscribe[ResponseType](ctx, c.bus, channels.RPCChannel(c.serviceName, rpc, topic), c.ChannelSize)
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
	sub, err := bus.SubscribeQueue[ResponseType](ctx, c.bus, channels.RPCChannel(c.serviceName, rpc, topic), c.ChannelSize)
	if err != nil {
		return nil, psrpc.NewError(psrpc.Internal, err)
	}
	return sub, nil
}
