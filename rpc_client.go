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

type RPCClient interface {
	// close all subscriptions and stop
	Close()

	rpcClientInternal
}

type rpcClientInternal interface {
	isRPCClient(*rpcClient)
}

func NewRPCClient(serviceName, clientID string, bus MessageBus, opts ...ClientOpt) (RPCClient, error) {
	c := &rpcClient{
		MessageBus:       bus,
		clientOpts:       getClientOpts(opts...),
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

type rpcClient struct {
	rpcClientInternal

	MessageBus
	clientOpts

	serviceName      string
	id               string
	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	closed           chan struct{}
}

func (c *rpcClient) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}

func RequestSingle[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	request proto.Message,
	opts ...RequestOpt,
) (ResponseType, error) {
	return RequestTopicSingle[ResponseType](ctx, client, rpc, "", request, opts...)
}

func RequestTopicSingle[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	topic string,
	request proto.Message,
	opts ...RequestOpt,
) (ResponseType, error) {

	c := client.(*rpcClient)
	o := getRequestOpts(c.clientOpts, opts...)
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

	if err = Publish(c, ctx, getRPCChannel(c.serviceName, rpc, topic), req); err != nil {
		return empty, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.selectionOpts)
	if err != nil {
		return empty, err
	}
	if err = Publish(c, ctx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
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

func selectServer(ctx context.Context, claimChan chan *internal.ClaimRequest, opts SelectionOpts) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.AffinityTimeout > 0 {
		time.AfterFunc(opts.AffinityTimeout, cancel)
	}

	serverID := ""
	best := float32(0)
	shorted := false

	for {
		select {
		case <-ctx.Done():
			if best == 0 {
				return "", errors.New("no valid servers found")
			} else {
				return serverID, nil
			}

		case claim := <-claimChan:
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
		}
	}
}

type Response[ResponseType proto.Message] struct {
	Result ResponseType
	Err    error
}

func RequestAll[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	request proto.Message,
	opts ...RequestOpt,
) (<-chan *Response[ResponseType], error) {
	return RequestTopicAll[ResponseType](ctx, client, rpc, "", request, opts...)
}

func RequestTopicAll[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	topic string,
	request proto.Message,
	opts ...RequestOpt,
) (<-chan *Response[ResponseType], error) {

	c := client.(*rpcClient)
	o := getRequestOpts(c.clientOpts, opts...)

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

	if err = Publish(c, ctx, getRPCChannel(c.serviceName, rpc, topic), req); err != nil {
		return nil, err
	}

	return responseChannel, nil
}

func SubscribeStream[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
) (Subscription[ResponseType], error) {
	return SubscribeTopic[ResponseType](ctx, client, rpc, "")
}

func SubscribeTopic[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	topic string,
) (Subscription[ResponseType], error) {
	c := client.(*rpcClient)
	return Subscribe[ResponseType](c, ctx, getRPCChannel(c.serviceName, rpc, topic))
}

func SubscribeStreamQueue[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
) (Subscription[ResponseType], error) {
	return SubscribeTopicQueue[ResponseType](ctx, client, rpc, "")
}

func SubscribeTopicQueue[ResponseType proto.Message](
	ctx context.Context,
	client RPCClient,
	rpc string,
	topic string,
) (Subscription[ResponseType], error) {
	c := client.(*rpcClient)
	return SubscribeQueue[ResponseType](c, ctx, getRPCChannel(c.serviceName, rpc, topic))
}
