package server

import (
	"context"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/channels"
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/internal/streams"
	"github.com/livekit/psrpc/pkg/metadata"
)

type StreamAffinityFunc func() float32

type streamRPCHandlerImpl[RecvType, SendType proto.Message] struct {
	mu           sync.RWMutex
	rpc          string
	topic        []string
	streamSub    bus.Subscription[*internal.Stream]
	claimSub     bus.Subscription[*internal.ClaimResponse]
	streams      map[string]streams.Stream[SendType, RecvType]
	claims       map[string]chan *internal.ClaimResponse
	affinityFunc StreamAffinityFunc
	requireClaim bool
	handler      func(psrpc.ServerStream[SendType, RecvType]) error
	draining     atomic.Bool
	complete     chan struct{}
	onCompleted  func()
	closeOnce    sync.Once
}

func newStreamRPCHandler[RecvType, SendType proto.Message](
	s *RPCServer,
	rpc string,
	topic []string,
	svcImpl func(psrpc.ServerStream[SendType, RecvType]) error,
	interceptor psrpc.ServerRPCInterceptor,
	affinityFunc StreamAffinityFunc,
	requireClaim bool,
) (*streamRPCHandlerImpl[RecvType, SendType], error) {

	ctx := context.Background()
	streamSub, err := bus.Subscribe[*internal.Stream](
		ctx, s.bus, channels.StreamServerChannel(s.serviceName, rpc, topic), s.ChannelSize,
	)
	if err != nil {
		return nil, err
	}

	var claimSub bus.Subscription[*internal.ClaimResponse]
	if requireClaim {
		claimSub, err = bus.Subscribe[*internal.ClaimResponse](
			ctx, s.bus, channels.ClaimResponseChannel(s.serviceName, rpc, topic), s.ChannelSize,
		)
		if err != nil {
			_ = streamSub.Close()
			return nil, err
		}
	} else {
		claimSub = bus.EmptySubscription[*internal.ClaimResponse]{}
	}

	h := &streamRPCHandlerImpl[RecvType, SendType]{
		rpc:          rpc,
		topic:        topic,
		streamSub:    streamSub,
		claimSub:     claimSub,
		streams:      make(map[string]streams.Stream[SendType, RecvType]),
		claims:       make(map[string]chan *internal.ClaimResponse),
		affinityFunc: affinityFunc,
		requireClaim: requireClaim,
		handler:      svcImpl,
		complete:     make(chan struct{}),
	}
	return h, nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) run(s *RPCServer) {
	go func() {
		requests := h.streamSub.Channel()
		claims := h.claimSub.Channel()

		for {
			select {
			case <-h.complete:
				return

			case is := <-requests:
				if is == nil {
					continue
				}
				if time.Now().UnixNano() < is.Expiry {
					if err := h.handleRequest(s, is); err != nil {
						logger.Error(err, "failed to handle request", "requestID", is.RequestId)
					}
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

func (h *streamRPCHandlerImpl[RecvType, SendType]) handleRequest(
	s *RPCServer,
	is *internal.Stream,
) error {
	if open := is.GetOpen(); open != nil {
		if h.draining.Load() {
			return nil
		}

		go func() {
			if err := h.handleOpenRequest(s, is, open); err != nil {
				logger.Error(err, "stream handler failed", "requestID", is.RequestId)
			}
		}()
	} else {
		h.mu.Lock()
		stream, ok := h.streams[is.StreamId]
		h.mu.Unlock()

		if ok {
			return stream.HandleStream(is)
		}
	}
	return nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) handleOpenRequest(
	s *RPCServer,
	is *internal.Stream,
	open *internal.StreamOpen,
) error {
	info := psrpc.RPCInfo{
		Service: s.serviceName,
		Method:  h.rpc,
		Topic:   h.topic,
	}

	head := &metadata.Header{
		RemoteID: open.NodeId,
		SentAt:   time.UnixMilli(is.SentAt),
		Metadata: open.Metadata,
	}
	ctx := metadata.NewContextWithIncomingHeader(context.Background(), head)
	octx, cancel := context.WithDeadline(ctx, time.Unix(0, is.Expiry))
	defer cancel()

	if h.requireClaim {
		claimed, err := h.claimRequest(s, octx, is)
		if !claimed {
			return err
		}
	}

	stream := streams.NewStream[SendType, RecvType](
		ctx,
		s.Timeout,
		is.StreamId,
		&serverStream[RecvType, SendType]{
			h:      h,
			s:      s,
			nodeID: open.NodeId,
		},
		info,
		s.StreamInterceptors,
		make(chan RecvType, s.ChannelSize),
		make(map[string]chan struct{}),
	)

	h.mu.Lock()
	h.streams[is.StreamId] = stream
	h.mu.Unlock()

	if err := stream.Ack(octx, is); err != nil {
		_ = stream.Close(err)
		return err
	}

	err := h.handler(stream)
	if !stream.Hijacked() {
		_ = stream.Close(err)
	}
	return nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) claimRequest(
	s *RPCServer,
	ctx context.Context,
	is *internal.Stream,
) (bool, error) {

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc()
		if affinity < 0 {
			return false, nil
		}
	} else {
		affinity = 1
	}

	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	h.mu.Lock()
	h.claims[is.RequestId] = claimResponseChan
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.claims, is.RequestId)
		h.mu.Unlock()
	}()

	err := s.bus.Publish(ctx, channels.ClaimRequestChannel(s.serviceName, is.GetOpen().NodeId), &internal.ClaimRequest{
		RequestId: is.RequestId,
		ServerId:  s.id,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

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

func (h *streamRPCHandlerImpl[RecvType, SendType]) close(force bool) {
	h.closeOnce.Do(func() {
		h.draining.Store(true)

		h.mu.Lock()
		serverStreams := maps.Values(h.streams)
		h.mu.Unlock()

		var wg sync.WaitGroup
		for _, s := range serverStreams {
			wg.Add(1)
			s := s
			go func() {
				_ = s.Close(psrpc.ErrStreamEOF)
				wg.Done()
			}()
		}
		wg.Wait()

		_ = h.streamSub.Close()
		_ = h.claimSub.Close()
		h.onCompleted()
		close(h.complete)
	})
	<-h.complete
}

type serverStream[SendType, RecvType proto.Message] struct {
	h      *streamRPCHandlerImpl[SendType, RecvType]
	s      *RPCServer
	nodeID string
}

func (s *serverStream[RequestType, ResponseType]) Send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.s.bus.Publish(ctx, channels.StreamChannel(s.s.serviceName, s.nodeID), msg); err != nil {
		err = psrpc.NewError(psrpc.Internal, err)
	}
	return
}

func (s *serverStream[RequestType, ResponseType]) Close(streamID string) {
	s.h.mu.Lock()
	delete(s.h.streams, streamID)
	s.h.mu.Unlock()
}
