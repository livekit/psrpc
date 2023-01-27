package psrpc

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"

	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

type Stream[SendType, RecvType proto.Message] interface {
	Channel() <-chan *Response[RecvType]
	Send(msg SendType, opts ...StreamOption) error
	Close(cause error) error
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
	c     *RPCClient
	rpc   string
	topic string
}

func (s *clientStream) send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.c.bus.Publish(ctx, getStreamServerChannel(s.c.serviceName, s.rpc, s.topic), msg); err != nil {
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
	s.h.handling.Dec()
}

type streamHandler[SendType, RecvType proto.Message] struct {
	*streamImpl[SendType, RecvType]
}

func (h *streamHandler[SendType, RecvType]) Recv(msg proto.Message, err error) error {
	return h.recv(msg, err)
}

func (h *streamHandler[SendType, RecvType]) Send(msg proto.Message, opts ...StreamOption) error {
	return h.send(msg)
}

func (h *streamHandler[SendType, RecvType]) Close(cause error) error {
	return h.close(cause)
}

type streamImpl[SendType, RecvType proto.Message] struct {
	streamOpts
	adapter   streamAdapter
	handler   StreamHandler
	ctx       context.Context
	cancelCtx context.CancelFunc
	recvChan  chan *Response[RecvType]
	streamID  string
	mu        sync.Mutex
	hijacked  bool
	pending   atomic.Int32
	acks      map[string]chan struct{}
	err       error
	closeOnce sync.Once
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
		v, err := b.Message.Message.UnmarshalNew()
		if err != nil {
			err = NewError(MalformedRequest, err)
			_ = s.handler.Close(err)
			return err
		}

		if err := s.handler.Recv(v, err); err != nil {
			return err
		}

		ctx, cancel := context.WithDeadline(s.ctx, time.Unix(0, is.Expiry))
		defer cancel()
		if err := s.ack(ctx, is); err != nil {
			return err
		}

	case *internal.Stream_Close:
		s.closeOnce.Do(func() {
			s.mu.Lock()
			s.err = newErrorFromResponse(b.Close.Code, b.Close.Error)
			s.mu.Unlock()

			s.waitForPending()
			s.adapter.close(s.streamID)
			s.cancelCtx()
			s.handler.Recv(nil, io.EOF)
		})
	}

	return nil
}

func (s *streamImpl[SendType, RecvType]) recv(msg proto.Message, err error) error {
	r := &Response[RecvType]{}
	r.Result, _ = msg.(RecvType)
	r.Err = err

	if r.Err == io.EOF {
		close(s.recvChan)
	} else {
		select {
		case s.recvChan <- r:
		default:
			return ErrSlowConsumer
		}
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
	var err error = ErrStreamClosed

	s.closeOnce.Do(func() {
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
		err = s.adapter.send(s.ctx, &internal.Stream{
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
		s.handler.Recv(nil, io.EOF)
	})
	return err
}

func (s *streamImpl[SendType, RecvType]) send(msg proto.Message, opts ...StreamOption) (err error) {
	s.pending.Inc()
	defer s.pending.Dec()

	o := getStreamOpts(s.streamOpts, opts...)

	v, err := anypb.New(msg)
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
				Message: v,
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
	}

	return
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

func (s *streamImpl[SendType, RecvType]) Channel() <-chan *Response[RecvType] {
	return s.recvChan
}

func (s *streamImpl[SendType, RecvType]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *streamImpl[RequestType, ResponseType]) Close(cause error) error {
	return s.handler.Close(cause)
}

func (s *streamImpl[SendType, RecvType]) Send(request SendType, opts ...StreamOption) (err error) {
	return s.handler.Send(request, opts...)
}
