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

var (
	ErrRequestTimedOut = NewError(DeadlineExceeded, errors.New("request timed out"))
	ErrNoResponse      = NewError(Unavailable, errors.New("no response from servers"))
)

func NewRPCClient(serviceName, clientID string, bus MessageBus, opts ...ClientOption) (*RPCClient, error) {
	c := &RPCClient{
		clientOpts:       getClientOpts(opts...),
		bus:              bus,
		serviceName:      serviceName,
		id:               clientID,
		claimRequests:    make(map[string]chan *internal.ClaimRequest),
		responseChannels: make(map[string]chan *internal.Response),
		closed:           make(chan struct{}),
	}

	ctx := context.Background()
	responses, err := Subscribe[*internal.Response](
		ctx, c.bus, getResponseChannel(serviceName, clientID), c.channelSize,
	)
	if err != nil {
		return nil, err
	}

	claims, err := Subscribe[*internal.ClaimRequest](
		ctx, c.bus, getClaimRequestChannel(serviceName, clientID), c.channelSize,
	)
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

type RPCClient struct {
	clientOpts

	bus              MessageBus
	serviceName      string
	id               string
	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	closed           chan struct{}
}

func (c *RPCClient) Close() {
	select {
	case <-c.closed:
	default:
		close(c.closed)
	}
}

func RequestSingle[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic string,
	request proto.Message,
	opts ...RequestOption,
) (ResponseType, error) {

	o := getRequestOpts(c.clientOpts, opts...)
	var empty ResponseType

	v, err := anypb.New(request)
	if err != nil {
		return empty, NewError(MalformedRequest, err)
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

	claimChan := make(chan *internal.ClaimRequest, c.channelSize)
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

	if err = c.bus.Publish(ctx, getRPCChannel(c.serviceName, rpc, topic), req); err != nil {
		return empty, NewError(Internal, err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.selectionOpts)
	if err != nil {
		return empty, err
	}
	if err = c.bus.Publish(ctx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
		RequestId: requestID,
		ServerId:  serverID,
	}); err != nil {
		return empty, NewError(Internal, err)
	}

	select {
	case resp := <-resChan:
		if resp.Error != "" {
			return empty, newErrorFromResponse(resp.Error, resp.Code)
		} else {
			response, err := resp.Response.UnmarshalNew()
			if err != nil {
				return empty, NewError(MalformedResponse, err)
			}
			return response.(ResponseType), nil
		}

	case <-ctx.Done():
		return empty, ErrRequestTimedOut
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
	claims := 0

	for {
		select {
		case <-ctx.Done():
			if best == 0 {
				if claims == 0 {
					return "", ErrNoResponse
				}
				return "", NewErrorf(Unavailable, "no servers available (received %d responses)", claims)
			} else {
				return serverID, nil
			}

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
		}
	}
}

type Response[ResponseType proto.Message] struct {
	Result ResponseType
	Err    error
}

func RequestMulti[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic string,
	request proto.Message,
	opts ...RequestOption,
) (<-chan *Response[ResponseType], error) {

	o := getRequestOpts(c.clientOpts, opts...)

	v, err := anypb.New(request)
	if err != nil {
		return nil, NewError(MalformedRequest, err)
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

	resChan := make(chan *internal.Response, c.channelSize)

	c.mu.Lock()
	c.responseChannels[requestID] = resChan
	c.mu.Unlock()

	responseChannel := make(chan *Response[ResponseType], c.channelSize)
	go func() {
		timer := time.NewTimer(o.timeout)
		for {
			select {
			case res := <-resChan:
				response := &Response[ResponseType]{}
				if res.Error != "" {
					response.Err = newErrorFromResponse(res.Error, res.Code)
				} else {
					v, err := res.Response.UnmarshalNew()
					if err != nil {
						response.Err = NewError(MalformedResponse, err)
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

	if err = c.bus.Publish(ctx, getRPCChannel(c.serviceName, rpc, topic), req); err != nil {
		return nil, NewError(Internal, err)
	}

	return responseChannel, nil
}

func Join[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic string,
) (Subscription[ResponseType], error) {
	sub, err := Subscribe[ResponseType](ctx, c.bus, getRPCChannel(c.serviceName, rpc, topic), c.channelSize)
	if err != nil {
		return nil, NewError(Internal, err)
	}
	return sub, nil
}

func JoinQueue[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic string,
) (Subscription[ResponseType], error) {
	sub, err := SubscribeQueue[ResponseType](ctx, c.bus, getRPCChannel(c.serviceName, rpc, topic), c.channelSize)
	if err != nil {
		return nil, NewError(Internal, err)
	}
	return sub, nil
}
