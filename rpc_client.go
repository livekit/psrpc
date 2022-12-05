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

	serviceName      string
	id               string
	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	closed           chan struct{}
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

func (c *rpcClient) SendSingleRequest(ctx context.Context, rpc string, request proto.Message, opts ...RequestOption) (proto.Message, error) {
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
		Multi:     false,
		Request:   v,
	}

	claimChan := make(chan *internal.ClaimRequest, o.channelSize)
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

	if err = c.Publish(ctx, getRequestChannel(c.serviceName, rpc), req); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.affinity)
	if err != nil {
		return nil, err
	}
	if err = c.Publish(ctx, getClaimResponseChannel(c.serviceName), &internal.ClaimResponse{
		RequestId: requestID,
		ServerId:  serverID,
	}); err != nil {
		return nil, err
	}

	select {
	case resp := <-resChan:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		} else {
			return resp.Response.UnmarshalNew()
		}

	case <-ctx.Done():
		return nil, errors.New("request timed out")
	}
}

func (c *rpcClient) SendMultiRequest(ctx context.Context, rpc string, request proto.Message, opts ...RequestOption) (<-chan proto.Message, error) {
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

	resChan := make(chan *internal.Response, o.channelSize)

	c.mu.Lock()
	c.responseChannels[requestID] = resChan
	c.mu.Unlock()

	responseChannel := make(chan proto.Message, o.channelSize)
	go func() {
		timer := time.NewTimer(o.timeout)
		for {
			select {
			case res := <-resChan:
				if res.Error == "" {
					response, err := res.Response.UnmarshalNew()
					if err != nil {
						logger.Error(err, "failed to unmarshal response")
					} else {
						responseChannel <- response
					}
				}

			case <-timer.C:
				c.mu.Lock()
				delete(c.responseChannels, requestID)
				c.mu.Unlock()
				close(responseChannel)
				return
			}
		}
	}()

	if err = c.Publish(ctx, getRequestChannel(c.serviceName, rpc), req); err != nil {
		return nil, err
	}

	return responseChannel, nil
}

func (c *rpcClient) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}
