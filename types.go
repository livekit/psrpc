package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

type RPCClient interface {
	// send a request to a single server, and receive one response
	SendSingleRequest(ctx context.Context, rpc string, request proto.Message, opts ...RequestOption) (proto.Message, error)
	// send a request to all servers, and receive one response per server
	SendMultiRequest(ctx context.Context, rpc string, request proto.Message, opts ...RequestOption) (<-chan *Response, error)
	// subscribe to a streaming rpc (all subscribed clients will receive every message)
	JoinStream(ctx context.Context, rpc string) (Subscription, error)
	// join a queue for a streaming rpc (each message is only received by a single client)
	JoinStreamQueue(ctx context.Context, rpc string) (Subscription, error)
	// close all subscriptions and stop
	Close()
}

type Response struct {
	Result proto.Message
	Err    error
}

type RPCServer interface {
	// register a rpc handler function
	RegisterHandler(rpc string, handlerFunc HandlerFunc, opts ...HandlerOption) error
	// publish updates to a streaming rpc
	PublishToStream(ctx context.Context, rpc string, message proto.Message) error
	// stop listening for requests for a rpc
	DeregisterHandler(rpc string) error
	// close all subscriptions and stop
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
