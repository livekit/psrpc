package client

import (
	"context"
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

	// request hooks
	for _, hook := range c.RequestHooks {
		hook(ctx, request, info)
	}

	resChan := make(chan *psrpc.Response[ResponseType], c.ChannelSize)
	m := &multiRPC[ResponseType]{
		c:         c,
		requestID: rand.NewRequestID(),
		info:      info,
		resChan:   resChan,
	}
	m.handler = interceptors.ChainClientInterceptors[psrpc.ClientMultiRPCHandler](
		c.MultiRPCInterceptors, info, m,
	)

	if err = m.handler.Send(ctx, request, opts...); err != nil {
		for _, hook := range c.ResponseHooks {
			hook(ctx, request, info, nil, err)
		}
		return
	}

	return resChan, nil
}

type multiRPC[ResponseType proto.Message] struct {
	c         *RPCClient
	requestID string
	info      psrpc.RPCInfo
	handler   psrpc.ClientMultiRPCHandler
	resChan   chan<- *psrpc.Response[ResponseType]
}

func (m *multiRPC[ResponseType]) Send(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) error {
	o := getRequestOpts(m.c.ClientOpts, opts...)

	b, err := bus.SerializePayload(req)
	if err != nil {
		return psrpc.NewError(psrpc.MalformedRequest, err)
	}

	now := time.Now()
	ir := &internal.Request{
		RequestId:  m.requestID,
		ClientId:   m.c.id,
		SentAt:     now.UnixNano(),
		Expiry:     now.Add(o.Timeout).UnixNano(),
		Multi:      true,
		RawRequest: b,
		Metadata:   metadata.OutgoingContextMetadata(ctx),
	}

	resChan := make(chan *internal.Response, m.c.ChannelSize)

	m.c.mu.Lock()
	m.c.responseChannels[m.requestID] = resChan
	m.c.mu.Unlock()

	go m.handleResponses(ctx, req, resChan, o)

	if err = m.c.bus.Publish(ctx, channels.RPCChannel(m.c.serviceName, m.info.Method, m.info.Topic), ir); err != nil {
		return psrpc.NewError(psrpc.Internal, err)
	}

	return nil
}

func (m *multiRPC[ResponseType]) handleResponses(
	ctx context.Context,
	req proto.Message,
	resChan chan *internal.Response,
	opts psrpc.RequestOpts,
) {
	timer := time.NewTimer(opts.Timeout)
	for {
		select {
		case res := <-resChan:
			var v ResponseType
			var err error
			if res.Error != "" {
				err = psrpc.NewErrorFromResponse(res.Code, res.Error)
			} else {
				v, err = bus.DeserializePayload[ResponseType](res.RawResponse)
				if err != nil {
					err = psrpc.NewError(psrpc.MalformedResponse, err)
				}
			}

			// response hooks
			for _, hook := range m.c.ResponseHooks {
				hook(ctx, req, m.info, v, err)
			}
			m.handler.Recv(v, err)

		case <-timer.C:
			m.handler.Close()
			return
		}
	}
}

func (m *multiRPC[ResponseType]) Recv(msg proto.Message, err error) {
	m.resChan <- &psrpc.Response[ResponseType]{
		Result: msg.(ResponseType),
		Err:    err,
	}
}

func (m *multiRPC[ResponseType]) Close() {
	m.c.mu.Lock()
	delete(m.c.responseChannels, m.requestID)
	m.c.mu.Unlock()
	close(m.resChan)
}
