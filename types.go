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
	RequestSingle(ctx context.Context, request RequestType, opts ...RequestOption) (ResponseType, error)
	// send a request to all servers, and receive one response per server
	RequestAll(ctx context.Context, request RequestType, opts ...RequestOption) (<-chan *Response[ResponseType], error)
	// subscribe to a streaming rpc (all subscribed clients will receive every message)
	JoinStream(ctx context.Context, rpc string) (Subscription[ResponseType], error)
	// join a queue for a streaming rpc (each message is only received by a single client)
	JoinStreamQueue(ctx context.Context, rpc string) (Subscription[ResponseType], error)
}

type Response[ResponseType proto.Message] struct {
	Result ResponseType
	Err    error
}

type Subscription[MessageType proto.Message] interface {
	Channel() <-chan MessageType
	Close() error
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
	// publish updates to a streaming rpc
	PublishToStream(ctx context.Context, rpc string, message proto.Message) error
	// stop listening for requests for a rpc
	DeregisterHandler(rpc string) error
	// close all subscriptions and stop
	Close()
}
