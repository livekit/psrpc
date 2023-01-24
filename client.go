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
	ErrStreamClosed    = NewError(Canceled, errors.New("stream closed"))
)

func NewRPCClient(serviceName, clientID string, bus MessageBus, opts ...ClientOption) (*RPCClient, error) {
	c := &RPCClient{
		clientOpts:       getClientOpts(opts...),
		bus:              bus,
		serviceName:      serviceName,
		id:               clientID,
		claimRequests:    make(map[string]chan *internal.ClaimRequest),
		responseChannels: make(map[string]chan *internal.Response),
		streamChannels:   make(map[string]chan *internal.Stream),
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

	var streams Subscription[*internal.Stream]
	if c.enableStreams {
		streams, err = Subscribe[*internal.Stream](
			ctx, c.bus, getStreamChannel(serviceName, clientID), c.channelSize,
		)
		if err != nil {
			_ = responses.Close()
			_ = claims.Close()
			return nil, err
		}
	} else {
		streams = SubscribeNil[*internal.Stream]()
	}

	go func() {
		for {
			select {
			case <-c.closed:
				_ = claims.Close()
				_ = responses.Close()
				_ = streams.Close()
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

			case msg, ok := <-streams.Channel():
				if !ok {
					continue
				}
				c.mu.RLock()
				streamChan, ok := c.streamChannels[msg.StreamId]
				c.mu.RUnlock()
				if ok {
					streamChan <- msg
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
	streamChannels   map[string]chan *internal.Stream
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
) (response ResponseType, err error) {

	o := getRequestOpts(c.clientOpts, opts...)
	info := RPCInfo{
		Method: rpc,
		Topic:  topic,
	}

	// response hooks
	defer func() {
		for _, hook := range c.responseHooks {
			hook(ctx, request, info, response, err)
		}
	}()

	// request hooks
	for _, hook := range c.requestHooks {
		hook(ctx, request, info)
	}

	v, err := anypb.New(request)
	if err != nil {
		err = NewError(MalformedRequest, err)
		return
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
		err = NewError(Internal, err)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.selectionOpts)
	if err != nil {
		return
	}
	if err = c.bus.Publish(ctx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
		RequestId: requestID,
		ServerId:  serverID,
	}); err != nil {
		err = NewError(Internal, err)
		return
	}

	select {
	case res := <-resChan:
		if res.Error != "" {
			err = newErrorFromResponse(res.Code, res.Error)
		} else {
			var r proto.Message
			r, err = res.Response.UnmarshalNew()
			if err != nil {
				err = NewError(MalformedResponse, err)
			} else {
				response = r.(ResponseType)
			}
		}

	case <-ctx.Done():
		err = ErrRequestTimedOut
	}

	return
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
) (rChan <-chan *Response[ResponseType], err error) {

	o := getRequestOpts(c.clientOpts, opts...)
	info := RPCInfo{
		Method: rpc,
		Topic:  topic,
	}

	// call response hooks on internal failure
	defer func() {
		if err != nil {
			for _, hook := range c.responseHooks {
				hook(ctx, request, info, nil, err)
			}
		}
	}()

	// request hooks
	for _, hook := range c.requestHooks {
		hook(ctx, request, info)
	}

	v, err := anypb.New(request)
	if err != nil {
		err = NewError(MalformedRequest, err)
		return
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
				r := &Response[ResponseType]{}
				if res.Error != "" {
					r.Err = newErrorFromResponse(res.Code, res.Error)
				} else {
					v, err := res.Response.UnmarshalNew()
					if err != nil {
						r.Err = NewError(MalformedResponse, err)
					} else {
						r.Result = v.(ResponseType)
					}
				}

				// response hooks
				for _, hook := range c.responseHooks {
					hook(ctx, request, info, r.Result, r.Err)
				}
				responseChannel <- r

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
		err = NewError(Internal, err)
		return
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

func OpenStream[SendType, RecvType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic string,
	opts ...RequestOption,
) (stream ClientStream[SendType, RecvType], err error) {

	o := getRequestOpts(c.clientOpts, opts...)

	streamID := newStreamID()
	requestID := newRequestID()
	now := time.Now()
	req := &internal.Stream{
		StreamId:  streamID,
		RequestId: requestID,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.timeout).UnixNano(),
		Body: &internal.Stream_Open{
			Open: &internal.StreamOpen{
				NodeId: c.id,
			},
		},
	}

	claimChan := make(chan *internal.ClaimRequest, c.channelSize)
	recvChan := make(chan *internal.Stream, 1)

	c.mu.Lock()
	c.claimRequests[requestID] = claimChan
	c.streamChannels[streamID] = recvChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.claimRequests, streamID)
		c.mu.Unlock()
	}()

	if err = c.bus.Publish(ctx, getStreamServerChannel(c.serviceName, rpc, topic), req); err != nil {
		return nil, NewError(Internal, err)
	}

	ctx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	serverID, err := selectServer(ctx, claimChan, o.selectionOpts)
	if err != nil {
		return
	}
	if err = c.bus.Publish(ctx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
		RequestId: requestID,
		ServerId:  serverID,
	}); err != nil {
		return nil, NewError(Internal, err)
	}

	ackChan := make(chan struct{})

	s := &streamImpl[SendType, RecvType]{
		adapter: &clientStream{
			c:     c,
			rpc:   rpc,
			topic: topic,
		},
		recvChan: make(chan *Response[RecvType], c.channelSize),
		streamID: streamID,
		acks:     map[string]chan struct{}{requestID: ackChan},
		done:     make(chan struct{}),
	}

	go runClientStream(c, s, recvChan)

	select {
	case <-ackChan:
		return s, nil

	case <-ctx.Done():
		return nil, ErrRequestTimedOut
	}
}

func runClientStream[SendType, RecvType proto.Message](
	c *RPCClient,
	s *streamImpl[SendType, RecvType],
	recvChan chan *internal.Stream,
) {
	for {
		select {
		case <-s.done:
			return

		case <-c.closed:
			s.Close(nil)
			return

		case is := <-recvChan:
			if time.Now().UnixNano() < is.Expiry {
				go func() {
					if err := s.handleStream(is); err != nil {
						logger.Error(err, "failed to handle request", "requestID", is.RequestId)
					}
				}()
			}
		}
	}
}
