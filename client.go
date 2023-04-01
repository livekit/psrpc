package psrpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

var (
	ErrRequestCanceled = NewError(Canceled, errors.New("request canceled"))
	ErrRequestTimedOut = NewError(DeadlineExceeded, errors.New("request timed out"))
	ErrNoResponse      = NewError(Unavailable, errors.New("no response from servers"))
	ErrStreamEOF       = NewError(Unavailable, io.EOF)
	ErrStreamClosed    = NewError(Canceled, errors.New("stream closed"))
	ErrSlowConsumer    = NewError(Unavailable, errors.New("stream message discarded by slow consumer"))
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
		streams = nilSubscription[*internal.Stream]{}
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

			case msg := <-streams.Channel():
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

func NewRPCClientWithStreams(serviceName, clientID string, bus MessageBus, opts ...ClientOption) (*RPCClient, error) {
	opts = append([]ClientOption{withStreams()}, opts...)
	return NewRPCClient(serviceName, clientID, bus, opts...)
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
	topic []string,
	requireClaim bool,
	request proto.Message,
	opts ...RequestOption,
) (response ResponseType, err error) {

	info := RPCInfo{
		Service: c.serviceName,
		Method:  rpc,
		Topic:   topic,
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

	call := func(ctx context.Context, request proto.Message, opts ...RequestOption) (response proto.Message, err error) {
		o := getRequestOpts(c.clientOpts, opts...)

		b, a, err := serializePayload(request)
		if err != nil {
			err = NewError(MalformedRequest, err)
			return
		}

		requestID := newRequestID()
		now := time.Now()
		req := &internal.Request{
			RequestId:  requestID,
			ClientId:   c.id,
			SentAt:     now.UnixNano(),
			Expiry:     now.Add(o.timeout).UnixNano(),
			Multi:      false,
			Request:    a,
			RawRequest: b,
			Metadata:   OutgoingContextMetadata(ctx),
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

		if requireClaim {
			serverID, err := selectServer(ctx, claimChan, resChan, o.selectionOpts)
			if err != nil {
				return nil, err
			}
			if err = c.bus.Publish(ctx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
				RequestId: requestID,
				ServerId:  serverID,
			}); err != nil {
				err = NewError(Internal, err)
				return nil, err
			}
		}

		select {
		case res := <-resChan:
			if res.Error != "" {
				err = newErrorFromResponse(res.Code, res.Error)
			} else {
				response, err = deserializePayload[ResponseType](res.RawResponse, res.Response)
				if err != nil {
					err = NewError(MalformedResponse, err)
				}
			}

		case <-ctx.Done():
			err = ctx.Err()
			if errors.Is(err, context.Canceled) {
				err = ErrRequestCanceled
			} else if errors.Is(err, context.DeadlineExceeded) {
				err = ErrRequestTimedOut
			}
		}

		return
	}

	res, err := chainClientInterceptors[RPCInterceptor](c.rpcInterceptors, info, call)(ctx, request, opts...)
	if res != nil {
		response, _ = res.(ResponseType)
	}
	return
}

func selectServer(
	ctx context.Context,
	claimChan chan *internal.ClaimRequest,
	resChan chan *internal.Response,
	opts SelectionOpts,
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
				return "", ErrNoResponse
			}
			return "", NewErrorf(Unavailable, "no servers available (received %d responses)", claims)

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
				resErr = NewErrorf(ErrorCode(res.Code), res.Error)
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
	topic []string,
	request proto.Message,
	opts ...RequestOption,
) (rChan <-chan *Response[ResponseType], err error) {

	info := RPCInfo{
		Service: c.serviceName,
		Method:  rpc,
		Topic:   topic,
		Multi:   true,
	}

	responseChannel := make(chan *Response[ResponseType], c.channelSize)
	call := &multiRPC[ResponseType]{
		c:         c,
		requestID: newRequestID(),
		resChan:   responseChannel,
		info:      info,
	}
	call.interceptor = chainClientInterceptors[MultiRPCInterceptor](c.multiRPCInterceptors, info, &multiRPCInterceptorRoot[ResponseType]{call})

	// request hooks
	for _, hook := range c.requestHooks {
		hook(ctx, request, info)
	}

	if err = call.interceptor.Send(ctx, request, opts...); err != nil {
		for _, hook := range c.responseHooks {
			hook(ctx, request, info, nil, err)
		}
		return
	}

	return responseChannel, nil
}

func Join[ResponseType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
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
	topic []string,
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
	topic []string,
	requireClaim bool,
	opts ...RequestOption,
) (ClientStream[SendType, RecvType], error) {

	o := getRequestOpts(c.clientOpts, opts...)
	info := RPCInfo{
		Service: c.serviceName,
		Method:  rpc,
		Topic:   topic,
	}

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
				NodeId:   c.id,
				Metadata: OutgoingContextMetadata(ctx),
			},
		},
	}

	claimChan := make(chan *internal.ClaimRequest, c.channelSize)
	recvChan := make(chan *internal.Stream, c.channelSize)

	c.mu.Lock()
	c.claimRequests[requestID] = claimChan
	c.streamChannels[streamID] = recvChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.claimRequests, requestID)
		c.mu.Unlock()
	}()

	octx, cancel := context.WithTimeout(ctx, o.timeout)
	defer cancel()

	if err := c.bus.Publish(octx, getStreamServerChannel(c.serviceName, rpc, topic), req); err != nil {
		return nil, NewError(Internal, err)
	}

	if requireClaim {
		serverID, err := selectServer(octx, claimChan, nil, o.selectionOpts)
		if err != nil {
			return nil, err
		}
		if err = c.bus.Publish(octx, getClaimResponseChannel(c.serviceName, rpc, topic), &internal.ClaimResponse{
			RequestId: requestID,
			ServerId:  serverID,
		}); err != nil {
			return nil, NewError(Internal, err)
		}
	}

	ackChan := make(chan struct{})

	stream := &streamImpl[SendType, RecvType]{
		streamOpts: streamOpts{
			timeout: c.timeout,
		},
		adapter: &clientStream{
			c:    c,
			info: info,
		},
		recvChan: make(chan RecvType, c.channelSize),
		streamID: streamID,
		acks:     map[string]chan struct{}{requestID: ackChan},
	}
	stream.ctx, stream.cancelCtx = context.WithCancel(ctx)
	stream.interceptor = chainClientInterceptors[StreamInterceptor](c.streamInterceptors, info, &streamInterceptorRoot[SendType, RecvType]{stream})

	go runClientStream(c, stream, recvChan)

	select {
	case <-ackChan:
		return stream, nil

	case <-octx.Done():
		err := octx.Err()
		if errors.Is(err, context.Canceled) {
			err = ErrRequestCanceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			err = ErrRequestTimedOut
		}
		_ = stream.Close(err)
		return nil, err
	}
}

func runClientStream[SendType, RecvType proto.Message](
	c *RPCClient,
	s *streamImpl[SendType, RecvType],
	recvChan chan *internal.Stream,
) {
	for {
		select {
		case <-s.ctx.Done():
			_ = s.Close(s.ctx.Err())
			return

		case <-c.closed:
			_ = s.Close(nil)
			return

		case is := <-recvChan:
			if time.Now().UnixNano() < is.Expiry {
				if err := s.handleStream(is); err != nil {
					logger.Error(err, "failed to handle request", "requestID", is.RequestId)
				}
			}
		}
	}
}
