package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

type handler[RequestType proto.Message, ResponseType proto.Message] struct {
	rpcHandlerInternal

	rpc          string
	handlerFunc  func(context.Context, RequestType) (ResponseType, error)
	affinityFunc AffinityFunc
	closed       chan struct{}
}

type rpcHandlerInternal interface {
	isHandler(*handler[proto.Message, proto.Message])

	getRPC() string
	getAffinityFunc() AffinityFunc
	handle(context.Context, proto.Message) (proto.Message, error)
	getClosed() chan struct{}
	close()
}

func NewHandler[RequestType proto.Message, ResponseType proto.Message](
	rpc string,
	handlerFunc func(context.Context, RequestType) (ResponseType, error),
) Handler {

	return &handler[RequestType, ResponseType]{
		rpc:         rpc,
		handlerFunc: handlerFunc,
		closed:      make(chan struct{}),
	}
}

func (h *handler[RequestType, ResponseType]) WithAffinityFunc(affinityFunc AffinityFunc) Handler {
	h.affinityFunc = affinityFunc
	return h
}

func (h *handler[RequestType, ResponseType]) getRPC() string {
	return h.rpc
}

func (h *handler[RequestType, ResponseType]) getAffinityFunc() AffinityFunc {
	return h.affinityFunc
}

func (h *handler[RequestType, ResponseType]) handle(ctx context.Context, req proto.Message) (proto.Message, error) {
	return h.handlerFunc(ctx, req.(RequestType))
}

func (h *handler[RequestType, ResponseType]) getClosed() chan struct{} {
	return h.closed
}

func (h *handler[RequestType, ResponseType]) close() {
	select {
	case <-h.closed:
	default:
		close(h.closed)
	}
}
