package psrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type Stream[SendType, RecvType proto.Message] interface {
	Channel() <-chan RecvType
	Send(msg SendType, opts ...StreamOption) error
	Close(cause error) error
	Context() context.Context
	Err() error
}

type ServerStream[SendType, RecvType proto.Message] interface {
	Stream[SendType, RecvType]
	Hijack()
}

type ClientStream[SendType, RecvType proto.Message] interface {
	Stream[SendType, RecvType]
}

type streamAdapter interface {
	send(ctx context.Context, msg *internal.Stream) error
	close(streamID string)
}

type clientStream struct {
	c    *RPCClient
	info RPCInfo
}

func (s *clientStream) send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.c.bus.Publish(ctx, getStreamServerChannel(s.c.serviceName, s.info.Method, s.info.Topic), msg); err != nil {
		err = NewError(Internal, err)
	}
	return
}

func (s *clientStream) close(streamID string) {
	s.c.mu.Lock()
	delete(s.c.streamChannels, streamID)
	s.c.mu.Unlock()
}

type serverStream[SendType, RecvType proto.Message] struct {
	h      *streamRPCHandlerImpl[SendType, RecvType]
	s      *RPCServer
	nodeID string
}

func (s *serverStream[RequestType, ResponseType]) send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.s.bus.Publish(ctx, getStreamChannel(s.s.serviceName, s.nodeID), msg); err != nil {
		err = NewError(Internal, err)
	}
	return
}

func (s *serverStream[RequestType, ResponseType]) close(streamID string) {
	s.h.mu.Lock()
	delete(s.h.streams, streamID)
	s.h.mu.Unlock()
}

type streamInterceptorRoot[SendType, RecvType proto.Message] struct {
	*streamImpl[SendType, RecvType]
}

func (h *streamInterceptorRoot[SendType, RecvType]) Recv(msg proto.Message) error {
	return h.recv(msg)
}

func (h *streamInterceptorRoot[SendType, RecvType]) Send(msg proto.Message, opts ...StreamOption) error {
	return h.send(msg)
}

func (h *streamInterceptorRoot[SendType, RecvType]) Close(cause error) error {
	return h.close(cause)
}

type streamImpl[SendType, RecvType proto.Message] struct {
	streamOpts
	adapter     streamAdapter
	interceptor StreamInterceptor
	ctx         context.Context
	cancelCtx   context.CancelFunc
	recvChan    chan RecvType
	streamID    string
	mu          sync.Mutex
	hijacked    bool
	pending     atomic.Int32
	acks        map[string]chan struct{}
	err         error
	closed      atomic.Bool
}

func (s *streamImpl[SendType, RecvType]) handleStream(is *internal.Stream) error {
	switch b := is.Body.(type) {
	case *internal.Stream_Ack:
		s.mu.Lock()
		ack, ok := s.acks[is.RequestId]
		delete(s.acks, is.RequestId)
		s.mu.Unlock()

		if ok {
			close(ack)
		}

	case *internal.Stream_Message:
		if s.closed.Load() {
			return ErrStreamClosed
		}

		s.pending.Inc()
		defer s.pending.Dec()

		v, err := deserializePayload[RecvType](b.Message.RawMessage)
		if err != nil {
			err = NewError(MalformedRequest, err)
			go s.interceptor.Close(err)
			return err
		}

		if err := s.interceptor.Recv(v); err != nil {
			return err
		}

		ctx, cancel := context.WithDeadline(s.ctx, time.Unix(0, is.Expiry))
		defer cancel()
		if err := s.ack(ctx, is); err != nil {
			return err
		}

	case *internal.Stream_Close:
		if !s.closed.Swap(true) {
			s.mu.Lock()
			s.err = newErrorFromResponse(b.Close.Code, b.Close.Error)
			s.mu.Unlock()

			s.adapter.close(s.streamID)
			s.cancelCtx()
			close(s.recvChan)
		}
	}

	return nil
}

func (s *streamImpl[SendType, RecvType]) recv(msg proto.Message) error {
	select {
	case s.recvChan <- msg.(RecvType):
	default:
		return ErrSlowConsumer
	}
	return nil
}

func (s *streamImpl[SendType, RecvType]) waitForPending() {
	for s.pending.Load() > 0 {
		time.Sleep(time.Millisecond * 100)
	}
}

func (s *streamImpl[SendType, RecvType]) ack(ctx context.Context, is *internal.Stream) error {
	return s.adapter.send(ctx, &internal.Stream{
		StreamId:  is.StreamId,
		RequestId: is.RequestId,
		SentAt:    is.SentAt,
		Expiry:    is.Expiry,
		Body: &internal.Stream_Ack{
			Ack: &internal.StreamAck{},
		},
	})
}

func (s *streamImpl[RequestType, ResponseType]) close(cause error) error {
	if s.closed.Swap(true) {
		return ErrStreamClosed
	}

	if cause == nil {
		cause = ErrStreamClosed
	}

	s.mu.Lock()
	s.err = cause
	s.mu.Unlock()

	msg := &internal.StreamClose{}
	var e Error
	if errors.As(cause, &e) {
		msg.Error = e.Error()
		msg.Code = string(e.Code())
	} else {
		msg.Error = cause.Error()
		msg.Code = string(Unknown)
	}

	now := time.Now()
	err := s.adapter.send(context.Background(), &internal.Stream{
		StreamId:  s.streamID,
		RequestId: newRequestID(),
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(s.timeout).UnixNano(),
		Body: &internal.Stream_Close{
			Close: msg,
		},
	})

	s.waitForPending()
	s.adapter.close(s.streamID)
	s.cancelCtx()
	close(s.recvChan)

	return err
}

func (s *streamImpl[SendType, RecvType]) send(msg proto.Message, opts ...StreamOption) (err error) {
	s.pending.Inc()
	defer s.pending.Dec()

	o := getStreamOpts(s.streamOpts, opts...)

	b, err := serializePayload(msg)
	if err != nil {
		err = NewError(MalformedRequest, err)
		return
	}

	ackChan := make(chan struct{})
	requestID := newRequestID()

	s.mu.Lock()
	s.acks[requestID] = ackChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.acks, requestID)
		s.mu.Unlock()
	}()

	now := time.Now()
	deadline := now.Add(o.timeout)

	ctx, cancel := context.WithDeadline(s.ctx, deadline)
	defer cancel()

	err = s.adapter.send(ctx, &internal.Stream{
		StreamId:  s.streamID,
		RequestId: requestID,
		SentAt:    now.UnixNano(),
		Expiry:    deadline.UnixNano(),
		Body: &internal.Stream_Message{
			Message: &internal.StreamMessage{
				RawMessage: b,
			},
		},
	})
	if err != nil {
		return
	}

	select {
	case <-ackChan:
	case <-ctx.Done():
		err = ErrRequestTimedOut
	case <-s.ctx.Done():
		err = s.Err()
	}

	return
}

func (s *streamImpl[SendType, RecvType]) Context() context.Context {
	return s.ctx
}

func (s *streamImpl[SendType, RecvType]) Hijacked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hijacked
}

func (s *streamImpl[SendType, RecvType]) Hijack() {
	s.mu.Lock()
	s.hijacked = true
	s.mu.Unlock()
}

func (s *streamImpl[SendType, RecvType]) Channel() <-chan RecvType {
	return s.recvChan
}

func (s *streamImpl[SendType, RecvType]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *streamImpl[RequestType, ResponseType]) Close(cause error) error {
	return s.interceptor.Close(cause)
}

func (s *streamImpl[SendType, RecvType]) Send(request SendType, opts ...StreamOption) (err error) {
	return s.interceptor.Send(request, opts...)
}
