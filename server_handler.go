package psrpc

import (
	"context"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type Handler func(context.Context, proto.Message) (proto.Message, error)

type AffinityFunc[RequestType proto.Message] func(RequestType) float32

type rpcHandler interface {
	close()
}

type rpcHandlerImpl[RequestType proto.Message, ResponseType proto.Message] struct {
	rpc          string
	topic        string
	affinityFunc AffinityFunc[RequestType]
	handler      func(context.Context, RequestType) (ResponseType, error)
	handling     atomic.Int32
	drain        func()
	onComplete   func()
}

func newRPCHandler[RequestType proto.Message, ResponseType proto.Message](
	rpc string,
	topic string,
	svcImpl func(context.Context, RequestType) (ResponseType, error),
	interceptor UnaryServerInterceptor,
	affinityFunc AffinityFunc[RequestType],
) *rpcHandlerImpl[RequestType, ResponseType] {
	h := &rpcHandlerImpl[RequestType, ResponseType]{
		rpc:          rpc,
		topic:        topic,
		affinityFunc: affinityFunc,
	}
	if interceptor == nil {
		h.handler = svcImpl
	} else {
		h.handler = func(ctx context.Context, req RequestType) (ResponseType, error) {
			res, err := interceptor(ctx, req, func(context.Context, proto.Message) (proto.Message, error) {
				return svcImpl(ctx, req)
			})
			return res.(ResponseType), err
		}
	}
	return h
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) handleRequest(
	s *RPCServer,
	ir *internal.Request,
) error {
	h.handling.Add(1)
	defer h.handling.Add(-1)

	ctx := context.Background()
	r, err := ir.Request.UnmarshalNew()
	if err != nil {
		var res ResponseType
		_ = h.sendResponse(s, ctx, ir, res, err)
		return err
	}
	req := r.(RequestType)

	if !ir.Multi {
		claimed, err := h.claimRequest(s, ctx, ir, req)
		if err != nil {
			return err
		} else if !claimed {
			return nil
		}
	}

	// call handler function and return response
	response, err := h.handler(ctx, req)
	return h.sendResponse(s, ctx, ir, response, err)
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) claimRequest(
	s *RPCServer,
	ctx context.Context,
	ir *internal.Request,
	req RequestType,
) (bool, error) {

	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	s.mu.Lock()
	s.claims[ir.RequestId] = claimResponseChan
	s.mu.Unlock()

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc(req)
	} else {
		affinity = 1
	}

	err := Publish(ctx, s.MessageBus, getClaimRequestChannel(s.serviceName, ir.ClientId), &internal.ClaimRequest{
		RequestId: ir.RequestId,
		ServerId:  s.id,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	defer func() {
		s.mu.Lock()
		delete(s.claims, ir.RequestId)
		s.mu.Unlock()
	}()

	timeout := time.Duration(ir.Expiry - time.Now().UnixNano())
	select {
	case claim := <-claimResponseChan:
		if claim.ServerId == s.id {
			return true, nil
		} else {
			return false, nil
		}

	case <-time.After(timeout):
		return false, nil
	}
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) sendResponse(
	s *RPCServer,
	ctx context.Context,
	ir *internal.Request,
	response proto.Message,
	err error,
) error {
	res := &internal.Response{
		RequestId: ir.RequestId,
		ServerId:  s.id,
		SentAt:    time.Now().UnixNano(),
	}

	if err != nil {
		res.Error = err.Error()
	} else if response != nil {
		v, err := anypb.New(response)
		if err != nil {
			return err
		}
		res.Response = v
	}

	return Publish(ctx, s.MessageBus, getResponseChannel(s.serviceName, ir.ClientId), res)
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) close() {
	h.drain()
	for h.handling.Load() > 0 {
		time.Sleep(time.Millisecond * 100)
	}
	h.onComplete()
}
