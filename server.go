package psrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"
)

type rpcHandler interface {
	close(force bool)
}

type RPCServer struct {
	serverOpts

	bus         MessageBus
	serviceName string
	id          string
	mu          sync.RWMutex
	handlers    map[string]rpcHandler
	active      atomic.Int32
	shutdown    chan struct{}
}

func NewRPCServer(serviceName, serverID string, bus MessageBus, opts ...ServerOption) *RPCServer {
	s := &RPCServer{
		serverOpts:  getServerOpts(opts...),
		bus:         bus,
		serviceName: serviceName,
		id:          serverID,
		handlers:    make(map[string]rpcHandler),
		shutdown:    make(chan struct{}),
	}

	return s
}

func RegisterHandler[RequestType proto.Message, ResponseType proto.Message](
	s *RPCServer,
	rpc string,
	topic []string,
	svcImpl func(context.Context, RequestType) (ResponseType, error),
	affinityFunc AffinityFunc[RequestType],
	requireClaim bool,
	multi bool,
) error {
	select {
	case <-s.shutdown:
		return errors.New("RPCServer closed")
	default:
	}

	key := getHandlerKey(rpc, topic)
	s.mu.RLock()
	_, ok := s.handlers[key]
	s.mu.RUnlock()
	if ok {
		return errors.New("handler already exists")
	}

	// create handler
	h, err := newRPCHandler(s, rpc, topic, svcImpl, s.chainedInterceptor, affinityFunc, requireClaim, multi)
	if err != nil {
		return err
	}

	s.active.Inc()
	h.onCompleted = func() {
		s.active.Dec()
		s.mu.Lock()
		delete(s.handlers, key)
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.handlers[key] = h
	s.mu.Unlock()

	h.run(s)
	return nil
}

func RegisterStreamHandler[RequestType proto.Message, ResponseType proto.Message](
	s *RPCServer,
	rpc string,
	topic []string,
	svcImpl func(ServerStream[ResponseType, RequestType]) error,
	affinityFunc StreamAffinityFunc,
	requireClaim bool,
) error {
	select {
	case <-s.shutdown:
		return errors.New("RPCServer closed")
	default:
	}

	key := getHandlerKey(rpc, topic)
	s.mu.RLock()
	_, ok := s.handlers[key]
	s.mu.RUnlock()
	if ok {
		return errors.New("handler already exists")
	}

	// create handler
	h, err := newStreamRPCHandler(s, rpc, topic, svcImpl, s.chainedInterceptor, affinityFunc, requireClaim)
	if err != nil {
		return err
	}

	s.active.Inc()
	h.onCompleted = func() {
		s.active.Dec()
		s.mu.Lock()
		delete(s.handlers, key)
		s.mu.Unlock()
	}

	s.mu.Lock()
	s.handlers[key] = h
	s.mu.Unlock()

	h.run(s)
	return nil
}

func (s *RPCServer) DeregisterHandler(rpc string, topic []string) {
	key := getHandlerKey(rpc, topic)
	s.mu.RLock()
	h, ok := s.handlers[key]
	s.mu.RUnlock()
	if ok {
		h.close(true)
	}
}

func (s *RPCServer) Publish(ctx context.Context, rpc string, topic []string, msg proto.Message) error {
	return s.bus.Publish(ctx, getRPCChannel(s.serviceName, rpc, topic), msg)
}

func (s *RPCServer) Close(force bool) {
	select {
	case <-s.shutdown:
	default:
		close(s.shutdown)

		s.mu.RLock()
		handlers := maps.Values(s.handlers)
		s.mu.RUnlock()

		var wg sync.WaitGroup
		for _, h := range handlers {
			wg.Add(1)
			h := h
			go func() {
				h.close(force)
				wg.Done()
			}()
		}
		wg.Wait()
	}
	if !force {
		for s.active.Load() > 0 {
			time.Sleep(time.Millisecond * 100)
		}
	}
}
