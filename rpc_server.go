package psrpc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type rpcServer struct {
	MessageBus
	rpcOpts

	id       string
	mu       sync.RWMutex
	handlers map[string]HandlerFunc
	claims   map[string]chan *internal.ClaimResponse
	closed   chan struct{}
}

func NewRPCServer(serverID string, bus MessageBus, opts ...RPCOption) (RPCServer, error) {
	s := &rpcServer{
		MessageBus: bus,
		rpcOpts:    getOpts(opts...),
		id:         serverID,
		handlers:   make(map[string]HandlerFunc),
		claims:     make(map[string]chan *internal.ClaimResponse),
		closed:     make(chan struct{}),
	}

	claims, err := s.Subscribe(context.Background(), "claims")
	if err != nil {
		return nil, err
	}

	go func() {
		for {
			select {
			case <-s.closed:
				_ = claims.Close()
				return

			case p := <-claims.Channel():
				claim := p.(*internal.ClaimResponse)

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

func (s *rpcServer) RegisterHandler(rpc string, handler HandlerFunc) error {
	s.mu.Lock()
	s.handlers[rpc] = handler
	s.mu.Unlock()

	sub, err := s.Subscribe(context.Background(), rpc)
	if err != nil {
		return err
	}
	reqChan := sub.Channel()

	go func() {
		for {
			select {
			case <-s.closed:
				_ = sub.Close()
				return

			case p := <-reqChan:
				go func(req *internal.Request) {
					err := s.handleRequest(rpc, req)
					if err != nil {
						fmt.Println(err)
					}
				}(p.(*internal.Request))
			}
		}
	}()

	return nil
}

func (s *rpcServer) handleRequest(rpc string, req *internal.Request) error {
	s.mu.RLock()
	handler, ok := s.handlers[rpc]
	s.mu.RUnlock()

	if !ok {
		return errors.New("handler not found")
	}

	ctx := context.Background()
	if !req.Multi {
		claimed, err := s.claimRequest(ctx, req)
		if err != nil {
			return err
		} else if !claimed {
			return nil
		}
	}

	request, err := anypb.UnmarshalNew(req.Request, proto.UnmarshalOptions{})
	if err != nil {
		return err
	}

	res := &internal.Response{
		RequestId: req.RequestId,
		HandlerId: s.id,
		SentAt:    time.Now().UnixNano(),
	}

	response, err := handler(ctx, request)
	if err != nil {
		res.Error = err.Error()
	} else {
		v, err := anypb.New(response)
		if err != nil {
			return err
		}
		res.Response = v
	}

	return s.Publish(ctx, req.SenderId, res)
}

func (s *rpcServer) claimRequest(ctx context.Context, request *internal.Request) (bool, error) {
	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	s.mu.Lock()
	s.claims[request.RequestId] = claimResponseChan
	s.mu.Unlock()

	err := s.Publish(ctx, "claims_"+request.SenderId, &internal.ClaimRequest{
		RequestId: request.RequestId,
		HandlerId: s.id,
		Available: true,
	})
	if err != nil {
		return false, err
	}

	defer func() {
		s.mu.Lock()
		delete(s.claims, request.RequestId)
		s.mu.Unlock()
	}()

	select {
	case claim := <-claimResponseChan:
		if claim.HandlerId == s.id {
			return true, nil
		} else {
			return false, nil
		}

	case <-time.After(s.requestTimeout):
		return false, errors.New("no response from server")
	}
}

func (s *rpcServer) Close() {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
}
