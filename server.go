package psrpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type RPCServer struct {
	MessageBus
	serverOpts

	serviceName string
	id          string
	mu          sync.RWMutex
	handlers    map[string]rpcHandler
	active      atomic.Int32
	claims      map[string]chan *internal.ClaimResponse
	shutdown    chan struct{}
}

func NewRPCServer(serviceName, serverID string, bus MessageBus, opts ...ServerOption) *RPCServer {
	s := &RPCServer{
		MessageBus:  bus,
		serverOpts:  getServerOpts(opts...),
		serviceName: serviceName,
		id:          serverID,
		handlers:    make(map[string]rpcHandler),
		claims:      make(map[string]chan *internal.ClaimResponse),
		shutdown:    make(chan struct{}),
	}

	return s
}

func RegisterHandler[RequestType proto.Message, ResponseType proto.Message](
	s *RPCServer,
	rpc string,
	topic string,
	svcImpl func(context.Context, RequestType) (ResponseType, error),
	affinityFunc AffinityFunc[RequestType],
) error {
	select {
	case <-s.shutdown:
		return errors.New("RPCServer closed")
	default:
	}

	key := getHandlerKey(rpc, topic)
	s.mu.RLock()
	_, ok := s.handlers[key]
	if ok {
		s.mu.RUnlock()
		return errors.New("handler already exists")
	}
	s.mu.RUnlock()

	ctx := context.Background()
	requests, err := Subscribe[*internal.Request](ctx, s, getRPCChannel(s.serviceName, rpc, topic))
	if err != nil {
		return err
	}

	claims, err := Subscribe[*internal.ClaimResponse](ctx, s, getClaimResponseChannel(s.serviceName, rpc, topic))
	if err != nil {
		_ = requests.Close()
		return err
	}
	complete := make(chan struct{})

	// create handler
	s.active.Add(1)
	h := newRPCHandler(rpc, topic, svcImpl, s.chainedUnaryInterceptors, affinityFunc)
	h.drain = func() {
		_ = requests.Close
	}
	h.onComplete = func() {
		_ = claims.Close()
		s.mu.Lock()
		delete(s.handlers, key)
		s.mu.Unlock()
		s.active.Add(-1)
		close(complete)
	}

	s.mu.Lock()
	s.handlers[key] = h
	s.mu.Unlock()

	reqChan := requests.Channel()
	go func() {
		for {
			select {
			case <-complete:
				return

			case ir := <-reqChan:
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

			case claim := <-claims.Channel():
				if claim == nil {
					continue
				}
				s.mu.RLock()
				claimChan, ok := s.claims[claim.RequestId]
				s.mu.RUnlock()
				if ok {
					claimChan <- claim
				}
			}
		}
	}()

	return nil
}

func (s *RPCServer) DeregisterHandler(rpc, topic string) {
	key := getHandlerKey(rpc, topic)
	s.mu.RLock()
	h, ok := s.handlers[key]
	s.mu.RUnlock()
	if ok {
		go h.close()
	}
}

func (s *RPCServer) Publish(ctx context.Context, rpc, topic string, msg proto.Message) error {
	return Publish(ctx, s.MessageBus, getRPCChannel(s.serviceName, rpc, topic), msg)
}

func (s *RPCServer) Close(force bool) {
	select {
	case <-s.shutdown:
	default:
		close(s.shutdown)
		s.mu.Lock()
		for _, h := range s.handlers {
			h.close()
		}
		s.mu.Unlock()
	}
	if !force {
		for s.active.Load() > 0 {
			time.Sleep(time.Millisecond * 100)
		}
	}
}
