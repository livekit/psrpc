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
