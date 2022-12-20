package psrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type rpcServer struct {
	MessageBus
	rpcOpts

	serviceName string
	id          string
	mu          sync.RWMutex
	handlers    map[string]Handler
	claims      map[string]chan *internal.ClaimResponse
	closed      chan struct{}
}

func NewRPCServer(serviceName, serverID string, bus MessageBus, opts ...RPCOption) (RPCServer, error) {
	s := &rpcServer{
		MessageBus:  bus,
		rpcOpts:     getRPCOpts(opts...),
		serviceName: serviceName,
		id:          serverID,
		handlers:    make(map[string]Handler),
		claims:      make(map[string]chan *internal.ClaimResponse),
		closed:      make(chan struct{}),
	}

	claims, err := Subscribe[*internal.ClaimResponse](s, context.Background(), getClaimResponseChannel(serviceName))
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case <-s.closed:
				_ = claims.Close()
				return

			case claim := <-claims.Channel():
				s.mu.RLock()
				claimChan, ok := s.claims[claim.RequestId]
				s.mu.RUnlock()
				if ok {
					claimChan <- claim
				}
			}
		}
	}()

	return s, nil
}

func (s *rpcServer) RegisterHandler(h Handler) error {
	rpc := h.getRPC()
	sub, err := Subscribe[*internal.Request](s, context.Background(), getRPCChannel(s.serviceName, rpc))
	if err != nil {
		return err
	}
	h.setSub(sub)

	s.mu.Lock()
	// close previous handler if exists
	if err = s.closeHandlerLocked(rpc); err != nil {
		s.mu.Unlock()
		return err
	}
	s.handlers[rpc] = h
	s.mu.Unlock()

	reqChan := sub.Channel()
	go func() {
		for {
			select {
			case <-s.closed:
				_ = sub.Close()
				return

			case req := <-reqChan:
				if req == nil {
					return
				}

				if time.Now().UnixNano() < req.Expiry {
					go func() {
						if err := s.handleRequest(h, req); err != nil {
							logger.Error(err, "failed to handle request", "requestID", req.RequestId)
						}
					}()
				}
			}
		}
	}()

	return nil
}

func (s *rpcServer) PublishToStream(ctx context.Context, rpc string, msg proto.Message) error {
	return Publish(s, ctx, getRPCChannel(s.serviceName, rpc), msg)
}

func (s *rpcServer) DeregisterHandler(rpc string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closeHandlerLocked(rpc)
}

func (s *rpcServer) Close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}

func (s *rpcServer) handleRequest(h Handler, req *internal.Request) error {
	ctx := context.Background()
	request, err := req.Request.UnmarshalNew()
	if err != nil {
		_ = s.sendResponse(ctx, req, nil, err)
		return err
	}

	if !req.Multi {
		affinity := float32(1)
		if af := h.getAffinityFunc(); af != nil {
			affinity = af(request)
		}

		claimed, err := s.claimRequest(ctx, req, affinity)
		if err != nil {
			return err
		} else if !claimed {
			return nil
		}
	}

	// call handler function and return response
	response, err := h.handle(ctx, request)
	return s.sendResponse(ctx, req, response, err)
}

func (s *rpcServer) claimRequest(ctx context.Context, request *internal.Request, affinity float32) (bool, error) {
	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	s.mu.Lock()
	s.claims[request.RequestId] = claimResponseChan
	s.mu.Unlock()

	err := Publish(s, ctx, getClaimRequestChannel(s.serviceName, request.ClientId), &internal.ClaimRequest{
		RequestId: request.RequestId,
		ServerId:  s.id,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	defer func() {
		s.mu.Lock()
		delete(s.claims, request.RequestId)
		s.mu.Unlock()
	}()

	timeout := time.Duration(request.Expiry - time.Now().UnixNano())
	select {
	case claim := <-claimResponseChan:
		if claim.ServerId == s.id {
			return true, nil
		} else {
			return false, nil
		}

	case <-time.After(timeout):
		return false, errors.New("no response from client")
	}
}

func (s *rpcServer) sendResponse(ctx context.Context, req *internal.Request, response proto.Message, err error) error {
	res := &internal.Response{
		RequestId: req.RequestId,
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

	return Publish(s, ctx, getResponseChannel(s.serviceName, req.ClientId), res)
}

func (s *rpcServer) closeHandlerLocked(rpc string) error {
	h, ok := s.handlers[rpc]
	if ok {
		delete(s.handlers, rpc)
		return h.close()
	}
	return nil
}
