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
	responses, err := Subscribe[*internal.Response](c, ctx, getResponseChannel(serviceName, clientID))
	if err != nil {
		return nil, err
	}

	claims, err := Subscribe[*internal.ClaimRequest](c, ctx, getClaimRequestChannel(serviceName, clientID))
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

			case claim := <-claims.Channel():
				c.mu.RLock()
				claimChan, ok := c.claimRequests[claim.RequestId]
				c.mu.RUnlock()
				if ok {
					claimChan <- claim
				}

			case res := <-responses.Channel():
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

func RequestSingle[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	request proto.Message,
	opts ...RequestOption,
) (ResponseType, error) {

	c := client.(*rpcClient)
	o := getRequestOpts(c.rpcOpts, opts...)
	var empty ResponseType

	v, err := anypb.New(request)
	if err != nil {
		return empty, err
	}

	requestID := newRequestID()
	now := time.Now()
	req := &internal.Request{
		RequestId: requestID,
		ClientId:  c.id,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.timeout).UnixNano(),
		Multi:     false,
		Request:   v,
	}

	claimChan := make(chan *internal.ClaimRequest, ChannelSize)
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

	if err = Publish(c, ctx, getRPCChannel(c.serviceName, rpc), req); err != nil {
		return empty, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.affinity)
	if err != nil {
		return empty, err
	}
	if err = Publish(c, ctx, getClaimResponseChannel(c.serviceName, rpc), &internal.ClaimResponse{
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

func RequestAll[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	request proto.Message,
	opts ...RequestOption,
) (<-chan *Response[ResponseType], error) {

	c := client.(*rpcClient)
	o := getRequestOpts(c.rpcOpts, opts...)

	v, err := anypb.New(request)
	if err != nil {
		return nil, err
	}

	requestID := newRequestID()
	now := time.Now()
	req := &internal.Request{
		RequestId: requestID,
		ClientId:  c.id,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.timeout).UnixNano(),
		Multi:     true,
		Request:   v,
	}

	resChan := make(chan *internal.Response, ChannelSize)

	c.mu.Lock()
	c.responseChannels[requestID] = resChan
	c.mu.Unlock()

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
				c.mu.Lock()
				delete(c.responseChannels, requestID)
				c.mu.Unlock()
				close(responseChannel)
				return
			}
		}
	}()

	if err = Publish(c, ctx, getRPCChannel(c.serviceName, rpc), req); err != nil {
		return nil, err
	}

	return responseChannel, nil
}

func JoinStream[ResponseType proto.Message](
	ctx context.Context, client RPCClient, rpc string,
) (Subscription[ResponseType], error) {

	c := client.(*rpcClient)
	return Subscribe[ResponseType](c, ctx, getRPCChannel(c.serviceName, rpc))
}

func JoinStreamQueue[ResponseType proto.Message](
	ctx context.Context, client RPCClient, rpc string,
) (Subscription[ResponseType], error) {

	c := client.(*rpcClient)
	return SubscribeQueue[ResponseType](c, ctx, getRPCChannel(c.serviceName, rpc))
}

func (c *rpcClient) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}
