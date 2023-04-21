package client

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/channels"
	"github.com/livekit/psrpc/internal/interceptors"
	"github.com/livekit/psrpc/internal/rand"
	"github.com/livekit/psrpc/pkg/metadata"
)

func RequestSingle[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
	requireClaim bool,
	request proto.Message,
	opts ...psrpc.RequestOption,
) (response ResponseType, err error) {

	info := psrpc.RPCInfo{
		Service: c.serviceName,
		Method:  rpc,
		Topic:   topic,
	}

	// response hooks
	defer func() {
		for _, hook := range c.ResponseHooks {
			hook(ctx, request, info, response, err)
		}
	}()

	// request hooks
	for _, hook := range c.RequestHooks {
		hook(ctx, request, info)
	}

	call := func(ctx context.Context, request proto.Message, opts ...psrpc.RequestOption) (response proto.Message, err error) {
		o := getRequestOpts(c.ClientOpts, opts...)

		b, err := bus.SerializePayload(request)
		if err != nil {
			err = psrpc.NewError(psrpc.MalformedRequest, err)
			return
		}

		requestID := rand.NewRequestID()
		now := time.Now()
		req := &internal.Request{
			RequestId:  requestID,
			ClientId:   c.id,
			SentAt:     now.UnixNano(),
			Expiry:     now.Add(o.Timeout).UnixNano(),
			Multi:      false,
			RawRequest: b,
			Metadata:   metadata.OutgoingContextMetadata(ctx),
		}

		claimChan := make(chan *internal.ClaimRequest, c.ChannelSize)
		resChan := make(chan *internal.Response, 1)

		c.mu.Lock()
		c.claimRequests[requestID] = claimChan
		c.responseChannels[requestID] = resChan
		c.mu.Unlock()

		defer func() {
			c.mu.Lock()
			delete(c.claimRequests, requestID)
			delete(c.responseChannels, requestID)
			c.mu.Unlock()
		}()

		if err = c.bus.Publish(ctx, channels.RPCChannel(c.serviceName, rpc, topic), req); err != nil {
			err = psrpc.NewError(psrpc.Internal, err)
			return
		}

		ctx, cancel := context.WithTimeout(ctx, o.Timeout)
		defer cancel()

		if requireClaim {
			serverID, err := selectServer(ctx, claimChan, resChan, o.SelectionOpts)
			if err != nil {
				return nil, err
			}
			if err = c.bus.Publish(ctx, channels.ClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
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

	res, err := interceptors.ChainClientInterceptors[psrpc.ClientRPCHandler](c.RpcInterceptors, info, call)(ctx, request, opts...)
	if res != nil {
		response, _ = res.(ResponseType)
	}
	return
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

	serverID := ""
	best := float32(0)
	shorted := false
	claims := 0
	var resErr error

	for {
		select {
		case <-ctx.Done():
			if best > 0 {
				return serverID, nil
			}
			if resErr != nil {
				return "", resErr
			}
			if claims == 0 {
				return "", psrpc.ErrNoResponse
			}
			return "", psrpc.NewErrorf(psrpc.Unavailable, "no servers available (received %d responses)", claims)

		case claim := <-claimChan:
			claims++
			if (opts.MinimumAffinity > 0 && claim.Affinity >= opts.MinimumAffinity && claim.Affinity > best) ||
				(opts.MinimumAffinity <= 0 && claim.Affinity > best) {
				if opts.AcceptFirstAvailable {
					return claim.ServerId, nil
				}

				serverID = claim.ServerId
				best = claim.Affinity

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

func RequestMulti[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
	request proto.Message,
	opts ...psrpc.RequestOption,
) (rChan <-chan *psrpc.Response[ResponseType], err error) {

	info := psrpc.RPCInfo{
		Service: c.serviceName,
		Method:  rpc,
		Topic:   topic,
		Multi:   true,
	}

	responseChannel := make(chan *psrpc.Response[ResponseType], c.ChannelSize)
	call := &multiRPC[ResponseType]{
		c:         c,
		requestID: rand.NewRequestID(),
		resChan:   responseChannel,
		info:      info,
	}
	call.handler = interceptors.ChainClientInterceptors[psrpc.ClientMultiRPCHandler](c.MultiRPCInterceptors, info, &multiRPCInterceptorRoot[ResponseType]{call})

	// request hooks
	for _, hook := range c.RequestHooks {
		hook(ctx, request, info)
	}

	if err = call.handler.Send(ctx, request, opts...); err != nil {
		for _, hook := range c.ResponseHooks {
			hook(ctx, request, info, nil, err)
		}
		return
	}

	return responseChannel, nil
}
