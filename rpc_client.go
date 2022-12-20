package psrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type rpcClient struct {
	MessageBus
	rpcOpts
	rpcClientInternal

	serviceName      string
	id               string
	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	closed           chan struct{}
}

type rpcClientInternal interface {
	isRPCClient(*rpcClient)
}

func NewRPCClient(serviceName, clientID string, bus MessageBus, opts ...RPCOption) (RPCClient, error) {
	c := &rpcClient{
		MessageBus:       bus,
		rpcOpts:          getRPCOpts(opts...),
		serviceName:      serviceName,
		id:               clientID,
		claimRequests:    make(map[string]chan *internal.ClaimRequest),
		responseChannels: make(map[string]chan *internal.Response),
		closed:           make(chan struct{}),
	}

	ctx := context.Background()
	responses, err := c.Subscribe(ctx, getResponseChannel(serviceName, clientID))
	if err != nil {
		return nil, err
	}

	claims, err := c.Subscribe(ctx, getClaimRequestChannel(serviceName, clientID))
	if err != nil {
		_ = responses.Close()
		return nil, err
	}

	go func() {
		for {
			select {
			case <-c.closed:
				_ = claims.Close()
				_ = responses.Close()
				return

			case p := <-claims.Channel():
				claim := p.(*internal.ClaimRequest)
				c.mu.RLock()
				claimChan, ok := c.claimRequests[claim.RequestId]
				c.mu.RUnlock()
				if ok {
					claimChan <- claim
				}

			case p := <-responses.Channel():
				res := p.(*internal.Response)
				c.mu.RLock()
				resChan, ok := c.responseChannels[res.RequestId]
				c.mu.RUnlock()
				if ok {
					resChan <- res
				}
			}
		}
	}()

	return c, nil
}

type rpcImpl[RequestType proto.Message, ResponseType proto.Message] struct {
	*rpcClient
	rpc string
}

func NewRPC[RequestType proto.Message, ResponseType proto.Message](c RPCClient, rpc string) RPC[RequestType, ResponseType] {
	return &rpcImpl[RequestType, ResponseType]{
		rpcClient: c.(*rpcClient),
		rpc:       rpc,
	}
}

func (r *rpcImpl[RequestType, ResponseType]) SendSingleRequest(ctx context.Context, request RequestType, opts ...RequestOption) (ResponseType, error) {
	o := getRequestOpts(r.rpcOpts, opts...)
	var empty ResponseType

	v, err := anypb.New(request)
	if err != nil {
		return empty, err
	}

	requestID := newRequestID()
	now := time.Now()
	req := &internal.Request{
		RequestId: requestID,
		ClientId:  r.id,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.timeout).UnixNano(),
		Multi:     false,
		Request:   v,
	}

	claimChan := make(chan *internal.ClaimRequest, ChannelSize)
	resChan := make(chan *internal.Response, 1)

	r.mu.Lock()
	r.claimRequests[requestID] = claimChan
	r.responseChannels[requestID] = resChan
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		delete(r.claimRequests, requestID)
		delete(r.responseChannels, requestID)
		r.mu.Unlock()
	}()

	if err = r.Publish(ctx, getRPCChannel(r.serviceName, r.rpc), req); err != nil {
		return empty, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.affinity)
	if err != nil {
		return empty, err
	}
	if err = r.Publish(ctx, getClaimResponseChannel(r.serviceName), &internal.ClaimResponse{
		RequestId: requestID,
		ServerId:  serverID,
	}); err != nil {
		return empty, err
	}

	select {
	case resp := <-resChan:
		if resp.Error != "" {
			return empty, errors.New(resp.Error)
		} else {
			response, err := resp.Response.UnmarshalNew()
			if err != nil {
				return empty, err
			}
			return response.(ResponseType), nil
		}

	case <-ctx.Done():
		return empty, errors.New("request timed out")
	}
}

func (r *rpcImpl[RequestType, ResponseType]) SendMultiRequest(ctx context.Context, request RequestType, opts ...RequestOption) (<-chan *Response[ResponseType], error) {
	o := getRequestOpts(r.rpcOpts, opts...)

	v, err := anypb.New(request)
	if err != nil {
		return nil, err
	}

	requestID := newRequestID()
	now := time.Now()
	req := &internal.Request{
		RequestId: requestID,
		ClientId:  r.id,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.timeout).UnixNano(),
		Multi:     true,
		Request:   v,
	}

	resChan := make(chan *internal.Response, ChannelSize)

	r.mu.Lock()
	r.responseChannels[requestID] = resChan
	r.mu.Unlock()

	responseChannel := make(chan *Response[ResponseType], ChannelSize)
	go func() {
		timer := time.NewTimer(o.timeout)
		for {
			select {
			case res := <-resChan:
				response := &Response[ResponseType]{}
				if res.Error != "" {
					response.Err = errors.New(res.Error)
				} else {
					v, err := res.Response.UnmarshalNew()
					if err != nil {
						response.Err = err
					} else {
						response.Result = v.(ResponseType)
					}
				}
				responseChannel <- response

			case <-timer.C:
				r.mu.Lock()
				delete(r.responseChannels, requestID)
				r.mu.Unlock()
				close(responseChannel)
				return
			}
		}
	}()

	if err = r.Publish(ctx, getRPCChannel(r.serviceName, r.rpc), req); err != nil {
		return nil, err
	}

	return responseChannel, nil
}

func (r *rpcImpl[RequestType, ResponseType]) JoinStream(ctx context.Context, rpc string) (Subscription, error) {
	return r.Subscribe(ctx, getRPCChannel(r.serviceName, rpc))
}

func (r *rpcImpl[RequestType, ResponseType]) JoinStreamQueue(ctx context.Context, rpc string) (Subscription, error) {
	return r.SubscribeQueue(ctx, getRPCChannel(r.serviceName, rpc))
}

func (c *rpcClient) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}
