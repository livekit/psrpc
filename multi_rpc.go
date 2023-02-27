package psrpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type multiRPCInterceptorRoot[ResponseType proto.Message] struct {
	*multiRPC[ResponseType]
}

func (m *multiRPCInterceptorRoot[ResponseType]) Send(ctx context.Context, msg proto.Message, opts ...RequestOption) error {
	return m.send(ctx, msg, opts...)
}

func (m *multiRPCInterceptorRoot[ResponseType]) Recv(msg proto.Message, err error) {
	m.recv(msg, err)
}

func (m *multiRPCInterceptorRoot[ResponseType]) Close() {
	m.close()
}

type multiRPC[ResponseType proto.Message] struct {
	c           *RPCClient
	resChan     chan<- *Response[ResponseType]
	interceptor MultiRPCInterceptor
	requestID   string
	info        RPCInfo
}

func (m *multiRPC[ResponseType]) send(ctx context.Context, msg proto.Message, opts ...RequestOption) (err error) {
	o := getRequestOpts(m.c.clientOpts, opts...)

	b, a, err := serializePayload(msg)
	if err != nil {
		err = NewError(MalformedRequest, err)
		return
	}

	now := time.Now()
	req := &internal.Request{
		RequestId:  m.requestID,
		ClientId:   m.c.id,
		SentAt:     now.UnixNano(),
		Expiry:     now.Add(o.timeout).UnixNano(),
		Multi:      true,
		Request:    a,
		RawRequest: b,
		Metadata:   OutgoingContextMetadata(ctx),
	}

	irChan := make(chan *internal.Response, m.c.channelSize)

	m.c.mu.Lock()
	m.c.responseChannels[m.requestID] = irChan
	m.c.mu.Unlock()

	go m.handleResponses(ctx, o, msg, irChan)

	if err = m.c.bus.Publish(ctx, getRPCChannel(m.c.serviceName, m.info.Method, m.info.Topic), req); err != nil {
		err = NewError(Internal, err)
	}
	return
}

func (m *multiRPC[ResponseType]) handleResponses(ctx context.Context, o reqOpts, msg proto.Message, irChan chan *internal.Response) {
	timer := time.NewTimer(o.timeout)
	for {
		select {
		case res := <-irChan:
			var v ResponseType
			var err error
			if res.Error != "" {
				err = newErrorFromResponse(res.Code, res.Error)
			} else {
				v, err = deserializePayload[ResponseType](res.RawResponse, res.Response)
				if err != nil {
					err = NewError(MalformedResponse, err)
				}
			}

			// response hooks
			for _, hook := range m.c.responseHooks {
				hook(ctx, msg, m.info, v, err)
			}
			m.interceptor.Recv(v, err)

		case <-timer.C:
			m.interceptor.Close()
			return
		}
	}
}

func (m *multiRPC[ResponseType]) recv(msg proto.Message, err error) {
	res := &Response[ResponseType]{}
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
