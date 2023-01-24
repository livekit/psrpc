package psrpc

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/livekit/psrpc/internal"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type Stream[SendType, RecvType proto.Message] interface {
	Channel() <-chan *Response[RecvType]
	Send(SendType) error
	Close(error) error
	Done() <-chan struct{}
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
	send(
		ctx context.Context,
		msg *internal.Stream,
	) error

	close(streamID string)
}

type clientStream struct {
	c     *RPCClient
	rpc   string
	topic string
}

func (s *clientStream) send(
	ctx context.Context,
	msg *internal.Stream,
) (err error) {
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

func (s *serverStream[RequestType, ResponseType]) send(
	ctx context.Context,
	msg *internal.Stream,
) (err error) {
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

type streamImpl[SendType, RecvType proto.Message] struct {
	adapter   streamAdapter
	recvChan  chan *Response[RecvType]
	streamID  string
	mu        sync.Mutex
	hijacked  bool
	acks      map[string]chan struct{}
	err       error
	done      chan struct{}
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
		r := &Response[RecvType]{}
		v, err := b.Message.Message.UnmarshalNew()
		if err != nil {
			r.Err = NewError(MalformedResponse, err)
		} else {
			r.Result = v.(RecvType)
		}

		s.ack(context.Background(), is)

		s.recvChan <- r

	case *internal.Stream_Close:
		s.mu.Lock()
		s.err = newErrorFromResponse(b.Close.Code, b.Close.Error)
		s.mu.Unlock()

		s.closeOnce.Do(func() {
			s.adapter.close(s.streamID)
			close(s.done)
		})
	}

	return nil
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

func (s *streamImpl[SendType, RecvType]) Done() <-chan struct{} {
	return s.done
}

func (s *streamImpl[SendType, RecvType]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *streamImpl[RequestType, ResponseType]) Close(reason error) error {
	err := errors.New("close called on closed stream")

	s.closeOnce.Do(func() {
		if reason == nil {
			reason = NewErrorf(Canceled, "stream closed")
		}

		s.mu.Lock()
		s.err = reason
		s.mu.Unlock()

		msg := &internal.StreamClose{}
		var e Error
		if errors.As(reason, &e) {
			msg.Error = e.Error()
			msg.Code = string(e.Code())
		} else {
			msg.Error = reason.Error()
			msg.Code = string(Unknown)
		}

		now := time.Now()
		err = s.adapter.send(context.Background(), &internal.Stream{
			StreamId:  s.streamID,
			RequestId: newRequestID(),
			SentAt:    now.UnixNano(),
			Expiry:    now.Add(DefaultClientTimeout).UnixNano(),
			Body: &internal.Stream_Close{
				Close: msg,
			},
		})

		s.adapter.close(s.streamID)
		close(s.done)
	})
	return err
}

func (s *streamImpl[SendType, RecvType]) Send(request SendType) (err error) {
	v, err := anypb.New(request)
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
	deadline := now.Add(DefaultClientTimeout)

	ctx, cancel := context.WithDeadline(context.Background(), deadline)
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
