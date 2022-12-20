package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type handler[RequestType proto.Message, ResponseType proto.Message] struct {
	rpcHandlerInternal

	rpc          string
	sub          Subscription[*internal.Request]
	handlerFunc  func(context.Context, RequestType) (ResponseType, error)
	affinityFunc AffinityFunc
}

type rpcHandlerInternal interface {
	isHandler(*handler[proto.Message, proto.Message])

	getRPC() string
	getAffinityFunc() AffinityFunc
	setSub(sub Subscription[*internal.Request])
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

func (h *handler[RequestType, ResponseType]) getRPC() string {
	return h.rpc
}

func (h *handler[RequestType, ResponseType]) getAffinityFunc() AffinityFunc {
	return h.affinityFunc
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
