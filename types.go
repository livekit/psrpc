package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

type RPCClient interface {
	SendSingleRequest(ctx context.Context, rpc string, request proto.Message) (proto.Message, error)
	SendMultiRequest(ctx context.Context, rpc string, request proto.Message) (<-chan proto.Message, error)
	Subscribe(ctx context.Context, channel string) (Subscription, error)
	SubscribeQueue(ctx context.Context, channel string) (Subscription, error)
	Close()
}

type RPCServer interface {
	RegisterHandler(rpc string, handler HandlerFunc) error
	Publish(ctx context.Context, channel string, message proto.Message) error
	Close()
}

type HandlerFunc func(ctx context.Context, request proto.Message) (proto.Message, error)

type MessageBus interface {
	Publish(ctx context.Context, channel string, msg proto.Message) error
	Subscribe(ctx context.Context, channel string) (Subscription, error)
	SubscribeQueue(ctx context.Context, channel string) (Subscription, error)
}

type Subscription interface {
	Channel() <-chan proto.Message
	Close() error
}
