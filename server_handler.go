package psrpc

import (
	"context"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type AffinityFunc[RequestType proto.Message] func(RequestType) float32

type rpcHandler interface {
	close()
}

type rpcHandlerImpl[RequestType proto.Message, ResponseType proto.Message] struct {
	mu           sync.RWMutex
	rpc          string
	topic        string
	requestSub   Subscription[*internal.Request]
	claimSub     Subscription[*internal.ClaimResponse]
	claims       map[string]chan *internal.ClaimResponse
	affinityFunc AffinityFunc[RequestType]
	handler      func(context.Context, RequestType) (ResponseType, error)
	handling     atomic.Int32
	complete     chan struct{}
	onCompleted  func()
}

func newRPCHandler[RequestType proto.Message, ResponseType proto.Message](
	s *RPCServer,
	rpc string,
	topic string,
	svcImpl func(context.Context, RequestType) (ResponseType, error),
	interceptor UnaryServerInterceptor,
	affinityFunc AffinityFunc[RequestType],
) (*rpcHandlerImpl[RequestType, ResponseType], error) {

	ctx := context.Background()
	requestSub, err := Subscribe[*internal.Request](
		ctx, s.bus, getRPCChannel(s.serviceName, rpc, topic), s.channelSize,
	)
	if err != nil {
		return nil, err
	}

	claimSub, err := Subscribe[*internal.ClaimResponse](
		ctx, s.bus, getClaimResponseChannel(s.serviceName, rpc, topic), s.channelSize,
	)
	if err != nil {
		_ = requestSub.Close()
		return nil, err
	}

	h := &rpcHandlerImpl[RequestType, ResponseType]{
		rpc:          rpc,
		topic:        topic,
		requestSub:   requestSub,
		claimSub:     claimSub,
		claims:       make(map[string]chan *internal.ClaimResponse),
		affinityFunc: affinityFunc,
		complete:     make(chan struct{}),
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

	return h, nil
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) run(s *RPCServer) {
	go func() {
		requests := h.requestSub.Channel()
		claims := h.claimSub.Channel()

		for {
			select {
			case <-h.complete:
				return

			case ir := <-requests:
				if ir == nil {
					continue
				}
				if time.Now().UnixNano() < ir.Expiry {
					go func() {
						if err := h.handleRequest(s, ir); err != nil {
							logger.Error(err, "failed to handle request", "requestID", ir.RequestId)
						}
					}()
				}

			case claim := <-claims:
				if claim == nil {
					continue
				}
				h.mu.RLock()
				claimChan, ok := h.claims[claim.RequestId]
				h.mu.RUnlock()
				if ok {
					claimChan <- claim
				}
			}
		}
	}()
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) handleRequest(
	s *RPCServer,
	ir *internal.Request,
) error {
	h.handling.Inc()
	defer h.handling.Dec()

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

	h.mu.Lock()
	h.claims[ir.RequestId] = claimResponseChan
	h.mu.Unlock()

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc(req)
	} else {
		affinity = 1
	}

	err := s.bus.Publish(ctx, getClaimRequestChannel(s.serviceName, ir.ClientId), &internal.ClaimRequest{
		RequestId: ir.RequestId,
		ServerId:  s.id,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	defer func() {
		h.mu.Lock()
		delete(h.claims, ir.RequestId)
		h.mu.Unlock()
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

	return s.bus.Publish(ctx, getResponseChannel(s.serviceName, ir.ClientId), res)
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) close() {
	_ = h.requestSub.Close()
	for h.handling.Load() > 0 {
		time.Sleep(time.Millisecond * 100)
	}
	_ = h.claimSub.Close()
	close(h.complete)
	h.onCompleted()
}
