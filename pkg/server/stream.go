// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/internal/stream"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/metadata"
)

type StreamAffinityFunc func(ctx context.Context) float32

type streamHandler[RecvType, SendType proto.Message] struct {
	i *info.RequestInfo

	handler      func(psrpc.ServerStream[SendType, RecvType]) error
	interceptors []psrpc.StreamInterceptor
	affinityFunc StreamAffinityFunc

	mu          sync.RWMutex
	streamSub   bus.Subscription[*internal.Stream]
	claimSub    bus.Subscription[*internal.ClaimResponse]
	streams     map[string]stream.Stream[SendType, RecvType]
	claims      map[string]chan *internal.ClaimResponse
	draining    atomic.Bool
	closeOnce   sync.Once
	complete    chan struct{}
	onCompleted func()
}

func newStreamRPCHandler[RecvType, SendType proto.Message](
	s *RPCServer,
	i *info.RequestInfo,
	svcImpl func(psrpc.ServerStream[SendType, RecvType]) error,
	affinityFunc StreamAffinityFunc,
) (*streamHandler[RecvType, SendType], error) {

	ctx := context.Background()
	streamSub, err := bus.Subscribe[*internal.Stream](
		ctx, s.bus, i.GetStreamServerChannel(), s.ChannelSize,
	)
	if err != nil {
		return nil, err
	}

	var claimSub bus.Subscription[*internal.ClaimResponse]
	if i.RequireClaim {
		claimSub, err = bus.Subscribe[*internal.ClaimResponse](
			ctx, s.bus, i.GetClaimResponseChannel(), s.ChannelSize,
		)
		if err != nil {
			_ = streamSub.Close()
			return nil, err
		}
	} else {
		claimSub = bus.EmptySubscription[*internal.ClaimResponse]{}
	}

	h := &streamHandler[RecvType, SendType]{
		i:            i,
		streamSub:    streamSub,
		claimSub:     claimSub,
		streams:      make(map[string]stream.Stream[SendType, RecvType]),
		claims:       make(map[string]chan *internal.ClaimResponse),
		affinityFunc: affinityFunc,
		handler:      svcImpl,
		complete:     make(chan struct{}),
	}
	return h, nil
}

func (h *streamHandler[RecvType, SendType]) run(s *RPCServer) {
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

func (h *streamHandler[RecvType, SendType]) handleRequest(
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
		ss, ok := h.streams[is.StreamId]
		h.mu.Unlock()

		if ok {
			return ss.HandleStream(is)
		}
	}
	return nil
}

func (h *streamHandler[RecvType, SendType]) handleOpenRequest(
	s *RPCServer,
	is *internal.Stream,
	open *internal.StreamOpen,
) error {
	head := &metadata.Header{
		RemoteID: open.NodeId,
		SentAt:   time.Unix(0, is.SentAt),
		Metadata: open.Metadata,
	}
	ctx := metadata.NewContextWithIncomingHeader(context.Background(), head)
	octx, cancel := context.WithDeadline(ctx, time.Unix(0, is.Expiry))
	defer cancel()

	if h.i.RequireClaim {
		claimed, err := h.claimRequest(s, octx, is)
		if !claimed {
			return err
		}
	}

	ss := stream.NewStream[SendType, RecvType](
		ctx,
		h.i,
		is.StreamId,
		s.Timeout,
		&serverStream[RecvType, SendType]{
			h:      h,
			s:      s,
			nodeID: open.NodeId,
		},
		s.StreamInterceptors,
		make(chan RecvType, s.ChannelSize),
		make(map[string]chan struct{}),
	)

	h.mu.Lock()
	h.streams[is.StreamId] = ss
	h.mu.Unlock()

	if err := ss.Ack(octx, is); err != nil {
		_ = ss.Close(err)
		return err
	}

	err := h.handler(ss)
	if !ss.Hijacked() {
		_ = ss.Close(err)
	}

	return nil
}

func (h *streamHandler[RecvType, SendType]) claimRequest(
	s *RPCServer,
	ctx context.Context,
	is *internal.Stream,
) (bool, error) {

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc(ctx)
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

	err := s.bus.Publish(ctx, info.GetClaimRequestChannel(s.Name, is.GetOpen().NodeId), &internal.ClaimRequest{
		RequestId: is.RequestId,
		ServerId:  s.ID,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	timeout := time.NewTimer(time.Duration(is.Expiry - time.Now().UnixNano()))
	defer timeout.Stop()

	select {
	case claim := <-claimResponseChan:
		if claim.ServerId == s.ID {
			return true, nil
		} else {
			return false, nil
		}

	case <-timeout.C:
		return false, nil
	}
}

func (h *streamHandler[RecvType, SendType]) close(force bool) {
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
	h      *streamHandler[SendType, RecvType]
	s      *RPCServer
	nodeID string
}

func (s *serverStream[RequestType, ResponseType]) Send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.s.bus.Publish(ctx, info.GetStreamChannel(s.s.Name, s.nodeID), msg); err != nil {
		err = psrpc.NewError(psrpc.Internal, err)
	}
	return
}

func (s *serverStream[RequestType, ResponseType]) Close(streamID string) {
	s.h.mu.Lock()
	delete(s.h.streams, streamID)
	s.h.mu.Unlock()
}
