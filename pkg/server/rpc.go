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
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/metadata"
)

type AffinityFunc[RequestType proto.Message] func(context.Context, RequestType) float32

type rpcHandlerImpl[RequestType proto.Message, ResponseType proto.Message] struct {
	i *info.RequestInfo

	handler      func(context.Context, RequestType) (ResponseType, error)
	affinityFunc AffinityFunc[RequestType]

	mu          sync.RWMutex
	requestSub  bus.Subscription[*internal.Request]
	claimSub    bus.Subscription[*internal.ClaimResponse]
	claims      map[string]chan *internal.ClaimResponse
	handling    sync.WaitGroup
	closeOnce   sync.Once
	complete    chan struct{}
	onCompleted func()
}

func newRPCHandler[RequestType proto.Message, ResponseType proto.Message](
	s *RPCServer,
	i *info.RequestInfo,
	svcImpl func(context.Context, RequestType) (ResponseType, error),
	interceptor psrpc.ServerRPCInterceptor,
	affinityFunc AffinityFunc[RequestType],
) (*rpcHandlerImpl[RequestType, ResponseType], error) {

	ctx := context.Background()

	var requestSub bus.Subscription[*internal.Request]
	var claimSub bus.Subscription[*internal.ClaimResponse]
	var err error

	if i.Queue {
		requestSub, err = bus.SubscribeQueue[*internal.Request](
			ctx, s.bus, i.GetRPCChannel(), s.ChannelSize,
		)
	} else {
		requestSub, err = bus.Subscribe[*internal.Request](
			ctx, s.bus, i.GetRPCChannel(), s.ChannelSize,
		)
	}
	if err != nil {
		return nil, err
	}

	if i.RequireClaim {
		claimSub, err = bus.Subscribe[*internal.ClaimResponse](
			ctx, s.bus, i.GetClaimResponseChannel(), s.ChannelSize,
		)
		if err != nil {
			_ = requestSub.Close()
			return nil, err
		}
	} else {
		claimSub = bus.EmptySubscription[*internal.ClaimResponse]{}
	}

	h := &rpcHandlerImpl[RequestType, ResponseType]{
		i:            i,
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
			var response ResponseType
			res, err := interceptor(ctx, req, i.RPCInfo, func(context.Context, proto.Message) (proto.Message, error) {
				return svcImpl(ctx, req)
			})
			if res != nil {
				response = res.(ResponseType)
			}
			return response, err
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
	h.handling.Add(1)
	defer h.handling.Done()

	head := &metadata.Header{
		RemoteID: ir.ClientId,
		SentAt:   time.Unix(0, ir.SentAt),
		Metadata: ir.Metadata,
	}
	ctx := metadata.NewContextWithIncomingHeader(context.Background(), head)
	ctx, cancel := context.WithDeadline(ctx, time.Unix(0, ir.Expiry))
	defer cancel()

	req, err := bus.DeserializePayload[RequestType](ir.RawRequest)
	if err != nil {
		var res ResponseType
		err = psrpc.NewError(psrpc.MalformedRequest, err)
		_ = h.sendResponse(s, ctx, ir, res, err)
		return err
	}

	if h.i.RequireClaim {
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

	var affinity float32
	if h.affinityFunc != nil {
		affinity = h.affinityFunc(ctx, req)
		if affinity < 0 {
			return false, nil
		}
	} else {
		affinity = 1
	}

	claimResponseChan := make(chan *internal.ClaimResponse, 1)

	h.mu.Lock()
	h.claims[ir.RequestId] = claimResponseChan
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.claims, ir.RequestId)
		h.mu.Unlock()
	}()

	err := s.bus.Publish(ctx, info.GetClaimRequestChannel(s.Name, ir.ClientId), &internal.ClaimRequest{
		RequestId: ir.RequestId,
		ServerId:  s.ID,
		Affinity:  affinity,
	})
	if err != nil {
		return false, err
	}

	timeout := time.NewTimer(time.Duration(ir.Expiry - time.Now().UnixNano()))
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

func (h *rpcHandlerImpl[RequestType, ResponseType]) sendResponse(
	s *RPCServer,
	ctx context.Context,
	ir *internal.Request,
	response proto.Message,
	err error,
) error {
	res := &internal.Response{
		RequestId: ir.RequestId,
		ServerId:  s.ID,
		SentAt:    time.Now().UnixNano(),
	}

	if err != nil {
		var e psrpc.Error

		if errors.As(err, &e) {
			res.Error = e.Error()
			res.Code = string(e.Code())
		} else {
			res.Error = err.Error()
			res.Code = string(psrpc.Unknown)
		}
	} else if response != nil {
		b, err := bus.SerializePayload(response)
		if err != nil {
			res.Error = err.Error()
			res.Code = string(psrpc.MalformedResponse)
		} else {
			res.RawResponse = b
		}
	}

	return s.bus.Publish(ctx, info.GetResponseChannel(s.Name, ir.ClientId), res)
}

func (h *rpcHandlerImpl[RequestType, ResponseType]) close(force bool) {
	h.closeOnce.Do(func() {
		_ = h.requestSub.Close()
		if !force {
			h.handling.Wait()
		}
		_ = h.claimSub.Close()
		h.onCompleted()
		close(h.complete)
	})
	<-h.complete
}
