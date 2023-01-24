package psrpc

import (
	"context"
	"log"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type streamRPCHandlerImpl[RecvType, SendType proto.Message] struct {
	mu           sync.RWMutex
	rpc          string
	topic        string
	streamSub    Subscription[*internal.Stream]
	claimSub     Subscription[*internal.ClaimResponse]
	streams      map[string]*streamImpl[SendType, RecvType]
	claims       map[string]chan *internal.ClaimResponse
	affinityFunc AffinityFunc[RecvType]
	handler      func(ServerStream[SendType, RecvType]) error
	handling     atomic.Int32
	complete     chan struct{}
	onCompleted  func()
}

func newStreamRPCHandler[RecvType, SendType proto.Message](
	s *RPCServer,
	rpc string,
	topic string,
	svcImpl func(ServerStream[SendType, RecvType]) error,
	interceptor ServerInterceptor,
	affinityFunc AffinityFunc[RecvType],
) (*streamRPCHandlerImpl[RecvType, SendType], error) {

	ctx := context.Background()
	streamSub, err := Subscribe[*internal.Stream](
		ctx, s.bus, getStreamServerChannel(s.serviceName, rpc, topic), s.channelSize,
	)
	if err != nil {
		return nil, err
	}

	claimSub, err := Subscribe[*internal.ClaimResponse](
		ctx, s.bus, getClaimResponseChannel(s.serviceName, rpc, topic), s.channelSize,
	)
	if err != nil {
		_ = streamSub.Close()
		return nil, err
	}

	h := &streamRPCHandlerImpl[RecvType, SendType]{
		rpc:          rpc,
		topic:        topic,
		streamSub:    streamSub,
		claimSub:     claimSub,
		streams:      make(map[string]*streamImpl[SendType, RecvType]),
		claims:       make(map[string]chan *internal.ClaimResponse),
		affinityFunc: affinityFunc,
		handler:      svcImpl,
		complete:     make(chan struct{}),
	}
	return h, nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) run(s *RPCServer) {
	go func() {
		streams := h.streamSub.Channel()
		claims := h.claimSub.Channel()

		for {
			select {
			case <-h.complete:
				return

			case is := <-streams:
				if is == nil {
					continue
				}
				log.Println("server", protojson.Format(is))
				if time.Now().UnixNano() < is.Expiry {
					go func() {
						if err := h.handleRequest(s, is); err != nil {
							logger.Error(err, "failed to handle request", "requestID", is.RequestId)
						}
					}()
				}

			case claim := <-claims:
				if claim == nil {
					continue
				}
				log.Println("server", protojson.Format(claim))
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

func (h *streamRPCHandlerImpl[RecvType, SendType]) handleRequest(
	s *RPCServer,
	is *internal.Stream,
) error {
	h.handling.Inc()

	ctx := context.Background()

	if open := is.GetOpen(); open != nil {
		octx, cancel := context.WithDeadline(ctx, time.Unix(0, is.Expiry))

		var req RecvType
		claimed, err := h.claimRequest(s, octx, is, req)
		defer cancel()
		if err != nil {
			return err
		} else if !claimed {
			return nil
		}

		stream := &streamImpl[SendType, RecvType]{
			adapter: &serverStream[RecvType, SendType]{
				h:      h,
				s:      s,
				nodeID: open.NodeId,
			},
			recvChan: make(chan *Response[RecvType], s.channelSize),
			streamID: is.StreamId,
			acks:     map[string]chan struct{}{},
			done:     make(chan struct{}),
		}

		if err := stream.ack(octx, is); err != nil {
			return err
		}

		h.mu.Lock()
		h.streams[is.StreamId] = stream
		h.mu.Unlock()

		err = h.handler(stream)
		if !stream.Hijacked() {
			_ = stream.Close(err)
		}
		return err
	}

	h.mu.Lock()
	stream, ok := h.streams[is.StreamId]
	h.mu.Unlock()

	if ok {
		stream.handleStream(is)
	}

	return nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) claimRequest(
	s *RPCServer,
	ctx context.Context,
	is *internal.Stream,
	req RecvType,
) (bool, error) {

	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	h.mu.Lock()
	h.claims[is.RequestId] = claimResponseChan
	h.mu.Unlock()

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc(req)
	} else {
		affinity = 1
	}

	err := s.bus.Publish(ctx, getClaimRequestChannel(s.serviceName, is.GetOpen().NodeId), &internal.ClaimRequest{
		RequestId: is.RequestId,
		ServerId:  s.id,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	defer func() {
		h.mu.Lock()
		delete(h.claims, is.RequestId)
		h.mu.Unlock()
	}()

	timeout := time.NewTimer(time.Duration(is.Expiry - time.Now().UnixNano()))
	defer timeout.Stop()

	select {
	case claim := <-claimResponseChan:
		if claim.ServerId == s.id {
			return true, nil
		} else {
			return false, nil
		}

	case <-timeout.C:
		return false, nil
	}
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) close() {
	_ = h.streamSub.Close()
	for h.handling.Load() > 0 {
		time.Sleep(time.Millisecond * 100)
	}
	_ = h.claimSub.Close()
	close(h.complete)
	h.onCompleted()
}
