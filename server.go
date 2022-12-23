package psrpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"
)

type RPCServer struct {
	MessageBus
	serverOpts

	serviceName string
	id          string
	mu          sync.RWMutex
	handlers    map[string]rpcHandler
	active      atomic.Int32
	shutdown    chan struct{}
}

func NewRPCServer(serviceName, serverID string, bus MessageBus, opts ...ServerOption) *RPCServer {
	s := &RPCServer{
		MessageBus:  bus,
		serverOpts:  getServerOpts(opts...),
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
	s.mu.RUnlock()
	if ok {
		return errors.New("handler already exists")
	}

	// create handler
	h, err := newRPCHandler(s, rpc, topic, svcImpl, s.chainedUnaryInterceptors, affinityFunc)
	if err != nil {
		return err
	}

	s.active.Add(1)
	h.onCompleted = func() {
		s.mu.Lock()
		delete(s.handlers, key)
		s.mu.Unlock()
		s.active.Add(-1)
	}

	s.mu.Lock()
	s.handlers[key] = h
	s.mu.Unlock()

	h.run(s)
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
