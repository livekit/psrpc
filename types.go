package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

// --- Client ---

type RPCClient interface {
	// close all subscriptions and stop
	Close()

	rpcClientInternal
}

type RPC[RequestType proto.Message, ResponseType proto.Message] interface {
	// send a request to a single server, and receive one response
	SendSingleRequest(ctx context.Context, request RequestType, opts ...RequestOption) (ResponseType, error)
	// send a request to all servers, and receive one response per server
	SendMultiRequest(ctx context.Context, request RequestType, opts ...RequestOption) (<-chan *Response[ResponseType], error)
	// subscribe to a streaming rpc (all subscribed clients will receive every message)
	JoinStream(ctx context.Context, rpc string) (Subscription, error)
	// join a queue for a streaming rpc (each message is only received by a single client)
	JoinStreamQueue(ctx context.Context, rpc string) (Subscription, error)
}

type Response[ResponseType proto.Message] struct {
	Result ResponseType
	Err    error
}

// --- Server ---

type Handler interface {
	// set affinity function
	WithAffinityFunc(affinityFunc AffinityFunc) Handler

	rpcHandlerInternal
}

type RPCServer interface {
	// register a handler
	RegisterHandler(h Handler) error
	// publish updates to a streaming rpcImpl
	PublishToStream(ctx context.Context, rpc string, message proto.Message) error
	// stop listening for requests for a rpcImpl
	DeregisterHandler(rpc string) error
	// close all subscriptions and stop
	Close()
}

// --- Bus ---

type MessageBus interface {
	Publish(ctx context.Context, channel string, msg proto.Message) error
	Subscribe(ctx context.Context, channel string) (Subscription, error)
	SubscribeQueue(ctx context.Context, channel string) (Subscription, error)
}

type Subscription interface {
	Channel() <-chan proto.Message
	Close() error
}
