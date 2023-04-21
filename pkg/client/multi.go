package client

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/channels"
	"github.com/livekit/psrpc/pkg/metadata"
)

type multiRPC[ResponseType proto.Message] struct {
	c         *RPCClient
	resChan   chan<- *psrpc.Response[ResponseType]
	handler   psrpc.ClientMultiRPCHandler
	requestID string
	info      psrpc.RPCInfo
}

type multiRPCInterceptorRoot[ResponseType proto.Message] struct {
	*multiRPC[ResponseType]
}

func (m *multiRPCInterceptorRoot[ResponseType]) Send(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) error {
	return m.send(ctx, msg, opts...)
}

func (m *multiRPCInterceptorRoot[ResponseType]) Recv(msg proto.Message, err error) {
	m.recv(msg, err)
}

func (m *multiRPCInterceptorRoot[ResponseType]) Close() {
	m.close()
}

func (m *multiRPC[ResponseType]) send(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) (err error) {
	o := getRequestOpts(m.c.ClientOpts, opts...)

	b, err := bus.SerializePayload(msg)
	if err != nil {
		err = psrpc.NewError(psrpc.MalformedRequest, err)
		return
	}

	now := time.Now()
	req := &internal.Request{
		RequestId:  m.requestID,
		ClientId:   m.c.id,
		SentAt:     now.UnixNano(),
		Expiry:     now.Add(o.Timeout).UnixNano(),
		Multi:      true,
		RawRequest: b,
		Metadata:   metadata.OutgoingContextMetadata(ctx),
	}

	irChan := make(chan *internal.Response, m.c.ChannelSize)

	m.c.mu.Lock()
	m.c.responseChannels[m.requestID] = irChan
	m.c.mu.Unlock()

	go m.handleResponses(ctx, o, msg, irChan)

	if err = m.c.bus.Publish(ctx, channels.RPCChannel(m.c.serviceName, m.info.Method, m.info.Topic), req); err != nil {
		err = psrpc.NewError(psrpc.Internal, err)
	}
	return
}

func (m *multiRPC[ResponseType]) handleResponses(ctx context.Context, o psrpc.RequestOpts, msg proto.Message, irChan chan *internal.Response) {
	timer := time.NewTimer(o.Timeout)
	for {
		select {
		case res := <-irChan:
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
				hook(ctx, msg, m.info, v, err)
			}
			m.handler.Recv(v, err)

		case <-timer.C:
			m.handler.Close()
			return
		}
	}
}

func (m *multiRPC[ResponseType]) recv(msg proto.Message, err error) {
	res := &psrpc.Response[ResponseType]{}
	res.Result, _ = msg.(ResponseType)
	res.Err = err
	m.resChan <- res
}

func (m *multiRPC[ResponseType]) close() {
	m.c.mu.Lock()
	delete(m.c.responseChannels, m.requestID)
	m.c.mu.Unlock()
	close(m.resChan)
}
