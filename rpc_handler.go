package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

type handler[RequestType proto.Message, ResponseType proto.Message] struct {
	rpc          string
	handlerFunc  func(context.Context, RequestType) (ResponseType, error)
	affinityFunc AffinityFunc
	sub          Subscription
}

type rpcHandlerInternal interface {
	getRPC() string
	setSub(Subscription)
	getAffinityFunc() AffinityFunc
	handle(context.Context, proto.Message) (proto.Message, error)
	close() error
}

func NewHandler[RequestType proto.Message, ResponseType proto.Message](
	rpc string,
	handlerFunc func(context.Context, RequestType) (ResponseType, error),
) Handler {
	return &handler[RequestType, ResponseType]{
		rpc:         rpc,
		handlerFunc: handlerFunc,
	}
}

func (h *handler[RequestType, ResponseType]) WithAffinityFunc(affinityFunc AffinityFunc) Handler {
	h.affinityFunc = affinityFunc
	return h
}

func (h *handler[RequestType, ResponseType]) isHandler() {}

func (h *handler[RequestType, ResponseType]) getRPC() string {
	return h.rpc
}

func (h *handler[RequestType, ResponseType]) setSub(sub Subscription) {
	h.sub = sub
}

func (h *handler[RequestType, ResponseType]) getAffinityFunc() AffinityFunc {
	return h.affinityFunc
}

func (h *handler[RequestType, ResponseType]) handle(ctx context.Context, req proto.Message) (proto.Message, error) {
	return h.handlerFunc(ctx, req.(RequestType))
}

func (h *handler[RequestType, ResponseType]) close() error {
	return h.sub.Close()
}
