package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type Handler interface {
	isHandler(*handler[proto.Message, proto.Message])

	getRPC() string
	setSub(Subscription[*internal.Request])
	getAffinity(proto.Message) float32
	handle(context.Context, proto.Message) (proto.Message, error)
	close() error
}

type handler[RequestType proto.Message, ResponseType proto.Message] struct {
	Handler

	rpc          string
	sub          Subscription[*internal.Request]
	handlerFunc  func(context.Context, RequestType) (ResponseType, error)
	affinityFunc AffinityFunc[RequestType]
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

func NewHandlerWithAffinity[RequestType proto.Message, ResponseType proto.Message](
	rpc string,
	handlerFunc func(context.Context, RequestType) (ResponseType, error),
	affinityFunc AffinityFunc[RequestType],
) Handler {
	return &handler[RequestType, ResponseType]{
		rpc:          rpc,
		handlerFunc:  handlerFunc,
		affinityFunc: affinityFunc,
	}
}

func (h *handler[RequestType, ResponseType]) getRPC() string {
	return h.rpc
}

func (h *handler[RequestType, ResponseType]) getAffinity(req proto.Message) float32 {
	if h.affinityFunc != nil {
		return h.affinityFunc(req.(RequestType))
	}
	return 1
}

func (h *handler[RequestType, ResponseType]) setSub(sub Subscription[*internal.Request]) {
	h.sub = sub
}

func (h *handler[RequestType, ResponseType]) handle(ctx context.Context, req proto.Message) (proto.Message, error) {
	return h.handlerFunc(ctx, req.(RequestType))
}

func (h *handler[RequestType, ResponseType]) close() error {
	return h.sub.Close()
}
