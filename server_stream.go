package psrpc

import (
	"context"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/exp/maps"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type StreamAffinityFunc func() float32

type streamRPCHandlerImpl[RecvType, SendType proto.Message] struct {
	mu           sync.RWMutex
	rpc          string
	topic        []string
	streamSub    Subscription[*internal.Stream]
	claimSub     Subscription[*internal.ClaimResponse]
	streams      map[string]*streamImpl[SendType, RecvType]
	claims       map[string]chan *internal.ClaimResponse
	affinityFunc StreamAffinityFunc
	requireClaim bool
	handler      func(ServerStream[SendType, RecvType]) error
	draining     atomic.Bool
	complete     chan struct{}
	onCompleted  func()
	closeOnce    sync.Once
}

func newStreamRPCHandler[RecvType, SendType proto.Message](
	s *RPCServer,
	rpc string,
	topic []string,
	svcImpl func(ServerStream[SendType, RecvType]) error,
	interceptor ServerInterceptor,
	affinityFunc StreamAffinityFunc,
	requireClaim bool,
) (*streamRPCHandlerImpl[RecvType, SendType], error) {

	ctx := context.Background()
	streamSub, err := Subscribe[*internal.Stream](
		ctx, s.bus, getStreamServerChannel(s.serviceName, rpc, topic), s.channelSize,
	)
	if err != nil {
		return nil, err
	}

	var claimSub Subscription[*internal.ClaimResponse]
	if requireClaim {
		claimSub, err = Subscribe[*internal.ClaimResponse](
			ctx, s.bus, getClaimResponseChannel(s.serviceName, rpc, topic), s.channelSize,
		)
		if err != nil {
			_ = streamSub.Close()
			return nil, err
		}
	} else {
		claimSub = nilSubscription[*internal.ClaimResponse]{}
	}

	h := &streamRPCHandlerImpl[RecvType, SendType]{
		rpc:          rpc,
		topic:        topic,
		streamSub:    streamSub,
		claimSub:     claimSub,
		streams:      make(map[string]*streamImpl[SendType, RecvType]),
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
			return stream.handleStream(is)
		}
	}
	return nil
}

func (h *streamRPCHandlerImpl[RecvType, SendType]) handleOpenRequest(
	s *RPCServer,
	is *internal.Stream,
	open *internal.StreamOpen,
) error {
	info := RPCInfo{
		Service: s.serviceName,
		Method:  h.rpc,
		Topic:   h.topic,
	}

	head := &Header{
		RemoteID: open.NodeId,
		SentAt:   time.UnixMilli(is.SentAt),
		Metadata: open.Metadata,
	}
	ctx := NewContextWithIncomingHeader(context.Background(), head)
	octx, cancel := context.WithDeadline(ctx, time.Unix(0, is.Expiry))
	defer cancel()

	if h.requireClaim {
		claimed, err := h.claimRequest(s, octx, is)
		if !claimed {
			return err
		}
	}

	stream := &streamImpl[SendType, RecvType]{
		streamOpts: streamOpts{
			timeout: s.timeout,
		},
		adapter: &serverStream[RecvType, SendType]{
			h:      h,
			s:      s,
			nodeID: open.NodeId,
		},
		recvChan: make(chan RecvType, s.channelSize),
		streamID: is.StreamId,
		acks:     map[string]chan struct{}{},
	}
	stream.ctx, stream.cancelCtx = context.WithCancel(ctx)
	stream.interceptor = chainClientInterceptors[StreamInterceptor](s.streamInterceptors, info, &streamInterceptorRoot[SendType, RecvType]{stream})

	h.mu.Lock()
	h.streams[is.StreamId] = stream
	h.mu.Unlock()

	if err := stream.ack(octx, is); err != nil {
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

	err := s.bus.Publish(ctx, getClaimRequestChannel(s.serviceName, is.GetOpen().NodeId), &internal.ClaimRequest{
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
		streams := maps.Values(h.streams)
		h.mu.Unlock()

		var wg sync.WaitGroup
		for _, s := range streams {
			wg.Add(1)
			s := s
			go func() {
				_ = s.Close(ErrStreamEOF)
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
