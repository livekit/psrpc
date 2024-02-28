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

package client

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/interceptors"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/metadata"
	"github.com/livekit/psrpc/pkg/rand"
)

func RequestSingle[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
	request proto.Message,
	opts ...psrpc.RequestOption,
) (response ResponseType, err error) {
	if c.closed.IsBroken() {
		err = psrpc.ErrClientClosed
		return
	}

	i := c.GetInfo(rpc, topic)

	// response hooks
	defer func() {
		for _, hook := range c.ResponseHooks {
			hook(ctx, request, i.RPCInfo, response, err)
		}
	}()

	// request hooks
	for _, hook := range c.RequestHooks {
		hook(ctx, request, i.RPCInfo)
	}

	reqInterceptors := getRequestInterceptors(
		c.RpcInterceptors,
		getRequestOpts(ctx, i, c.ClientOpts, opts...).Interceptors,
	)
	handler := interceptors.ChainClientInterceptors[psrpc.ClientRPCHandler](
		reqInterceptors, i, newRPC[ResponseType](c, i),
	)

	res, err := handler(ctx, request, opts...)
	if res != nil {
		response, _ = res.(ResponseType)
	}

	return
}

func newRPC[ResponseType proto.Message](c *RPCClient, i *info.RequestInfo) psrpc.ClientRPCHandler {
	return func(ctx context.Context, request proto.Message, opts ...psrpc.RequestOption) (response proto.Message, err error) {
		o := getRequestOpts(ctx, i, c.ClientOpts, opts...)

		b, err := bus.SerializePayload(request)
		if err != nil {
			err = psrpc.NewError(psrpc.MalformedRequest, err)
			return
		}

		requestID := rand.NewRequestID()
		now := time.Now()
		req := &internal.Request{
			RequestId:  requestID,
			ClientId:   c.ID,
			SentAt:     now.UnixNano(),
			Expiry:     now.Add(o.Timeout).UnixNano(),
			Multi:      false,
			RawRequest: b,
			Metadata:   metadata.OutgoingContextMetadata(ctx),
		}

		var claimChan chan *internal.ClaimRequest
		resChan := make(chan *internal.Response, 1)

		c.mu.Lock()
		if i.RequireClaim {
			claimChan = make(chan *internal.ClaimRequest, c.ChannelSize)
			c.claimRequests[requestID] = claimChan
		}
		c.responseChannels[requestID] = resChan
		c.mu.Unlock()

		defer func() {
			c.mu.Lock()
			if i.RequireClaim {
				delete(c.claimRequests, requestID)
			}
			delete(c.responseChannels, requestID)
			c.mu.Unlock()
		}()

		if err = c.bus.Publish(ctx, i.GetRPCChannel(), req); err != nil {
			err = psrpc.NewError(psrpc.Internal, err)
			return
		}

		ctx, cancel := context.WithTimeout(ctx, o.Timeout)
		defer cancel()

		if i.RequireClaim {
			serverID, err := selectServer(ctx, claimChan, resChan, o.SelectionOpts)
			if err != nil {
				return nil, err
			}

			if err = c.bus.Publish(ctx, i.GetClaimResponseChannel(), &internal.ClaimResponse{
				RequestId: requestID,
				ServerId:  serverID,
			}); err != nil {
				err = psrpc.NewError(psrpc.Internal, err)
				return nil, err
			}
		}

		select {
		case res := <-resChan:
			if res.Error != "" {
				err = psrpc.NewErrorFromResponse(res.Code, res.Error)
			} else {
				response, err = bus.DeserializePayload[ResponseType](res.RawResponse)
				if err != nil {
					err = psrpc.NewError(psrpc.MalformedResponse, err)
				}
			}

		case <-ctx.Done():
			err = ctx.Err()
			if errors.Is(err, context.Canceled) {
				err = psrpc.ErrRequestCanceled
			} else if errors.Is(err, context.DeadlineExceeded) {
				err = psrpc.ErrRequestTimedOut
			}
		}

		return
	}
}

func selectServer(
	ctx context.Context,
	claimChan chan *internal.ClaimRequest,
	resChan chan *internal.Response,
	opts psrpc.SelectionOpts,
) (string, error) {

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.AffinityTimeout > 0 {
		time.AfterFunc(opts.AffinityTimeout, cancel)
	}

	var (
		shorted    bool
		serverID   string
		affinity   float32
		claims     []*psrpc.Claim
		claimCount int
		resErr     error
	)

	for {
		select {
		case <-ctx.Done():
			switch {
			case opts.SelectionFunc != nil:
				return opts.SelectionFunc(claims)
			case serverID != "":
				return serverID, nil
			case resErr != nil:
				return "", resErr
			case claimCount > 0:
				return "", psrpc.NewErrorf(psrpc.Unavailable, "no servers available (received %d responses)", claimCount)
			default:
				return "", psrpc.ErrNoResponse
			}

		case claim := <-claimChan:
			claimCount++
			if (opts.MinimumAffinity > 0 && claim.Affinity >= opts.MinimumAffinity) || opts.MinimumAffinity <= 0 {
				if opts.AcceptFirstAvailable || opts.MaximumAffinity > 0 && claim.Affinity >= opts.MaximumAffinity {
					return claim.ServerId, nil
				}

				if opts.SelectionFunc != nil {
					claims = append(claims, &psrpc.Claim{ServerID: claim.ServerId, Affinity: claim.Affinity})
				} else if claim.Affinity > affinity {
					serverID = claim.ServerId
					affinity = claim.Affinity
				}

				if opts.ShortCircuitTimeout > 0 && !shorted {
					shorted = true
					time.AfterFunc(opts.ShortCircuitTimeout, cancel)
				}
			}

		case res := <-resChan:
			// will only happen with malformed requests
			if res.Error != "" {
				resErr = psrpc.NewErrorf(psrpc.ErrorCode(res.Code), res.Error)
			}
		}
	}
}
