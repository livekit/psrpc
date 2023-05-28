package stream

import (
	"context"
	"errors"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/interceptors"
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/internal/rand"
	"github.com/livekit/psrpc/pkg/info"
)

type Stream[SendType, RecvType proto.Message] interface {
	psrpc.ServerStream[SendType, RecvType]

	Ack(context.Context, *internal.Stream) error
	HandleStream(is *internal.Stream) error
	Hijacked() bool
}

type StreamAdapter interface {
	Send(ctx context.Context, msg *internal.Stream) error
	Close(streamID string)
}

type stream[SendType, RecvType proto.Message] struct {
	*streamBase[SendType, RecvType]

	handler  psrpc.StreamHandler
	hijacked bool
}

type streamBase[SendType, RecvType proto.Message] struct {
	psrpc.StreamOpts

	ctx      context.Context
	cancel   context.CancelFunc
	streamID string

	adapter  StreamAdapter
	recvChan chan RecvType

	mu      sync.Mutex
	pending sync.WaitGroup
	acks    map[string]chan struct{}
	closed  bool
	err     error
}

func NewStream[SendType, RecvType proto.Message](
	ctx context.Context,
	i *info.RequestInfo,
	streamID string,
	timeout time.Duration,
	adapter StreamAdapter,
	streamInterceptors []psrpc.StreamInterceptor,
	recvChan chan RecvType,
	acks map[string]chan struct{},
) Stream[SendType, RecvType] {

	ctx, cancel := context.WithCancel(ctx)
	base := &streamBase[SendType, RecvType]{
		StreamOpts: psrpc.StreamOpts{Timeout: timeout},
		ctx:        ctx,
		cancel:     cancel,
		streamID:   streamID,
		adapter:    adapter,
		recvChan:   recvChan,
		acks:       acks,
	}

	return &stream[SendType, RecvType]{
		streamBase: base,
		handler: interceptors.ChainClientInterceptors[psrpc.StreamHandler](
			streamInterceptors, i, base,
		),
	}
}

func (s *stream[SendType, RecvType]) HandleStream(is *internal.Stream) error {
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
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return s.err
		}

		s.pending.Add(1)
		defer s.pending.Done()
		s.mu.Unlock()

		v, err := bus.DeserializePayload[RecvType](b.Message.RawMessage)
		if err != nil {
			err = psrpc.NewError(psrpc.MalformedRequest, err)
			go func() {
				if e := s.handler.Close(err); e != nil {
					logger.Error(e, "failed to close stream")
				}
			}()
			return err
		}

		if err := s.handler.Recv(v); err != nil {
			return err
		}

		ctx, cancel := context.WithDeadline(s.ctx, time.Unix(0, is.Expiry))
		defer cancel()
		if err := s.Ack(ctx, is); err != nil {
			return err
		}

	case *internal.Stream_Close:
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return s.err
		}

		s.closed = true
		s.err = psrpc.NewErrorFromResponse(b.Close.Code, b.Close.Error)
		s.mu.Unlock()

		s.adapter.Close(s.streamID)
		s.cancel()
		close(s.recvChan)
	}

	return nil
}

func (s *stream[SendType, RecvType]) Context() context.Context {
	return s.ctx
}

func (s *stream[SendType, RecvType]) Channel() <-chan RecvType {
	return s.recvChan
}

func (s *stream[SendType, RecvType]) Ack(ctx context.Context, is *internal.Stream) error {
	return s.adapter.Send(ctx, &internal.Stream{
		StreamId:  is.StreamId,
		RequestId: is.RequestId,
		SentAt:    is.SentAt,
		Expiry:    is.Expiry,
		Body: &internal.Stream_Ack{
			Ack: &internal.StreamAck{},
		},
	})
}

func (s *stream[SendType, RecvType]) Recv(msg proto.Message) error {
	return s.handler.Recv(msg)
}

func (s *streamBase[SendType, RecvType]) Recv(msg proto.Message) error {
	select {
	case s.recvChan <- msg.(RecvType):
	default:
		return psrpc.ErrSlowConsumer
	}
	return nil
}

func (s *stream[SendType, RecvType]) Send(request SendType, opts ...psrpc.StreamOption) (err error) {
	return s.handler.Send(request, opts...)
}

func (s *streamBase[SendType, RecvType]) Send(msg proto.Message, opts ...psrpc.StreamOption) (err error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.err
	}

	s.pending.Add(1)
	defer s.pending.Done()
	s.mu.Unlock()

	o := getStreamOpts(s.StreamOpts, opts...)

	b, err := bus.SerializePayload(msg)
	if err != nil {
		err = psrpc.NewError(psrpc.MalformedRequest, err)
		return
	}

	ackChan := make(chan struct{})
	requestID := rand.NewRequestID()

	s.mu.Lock()
	s.acks[requestID] = ackChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.acks, requestID)
		s.mu.Unlock()
	}()

	now := time.Now()
	deadline := now.Add(o.Timeout)

	ctx, cancel := context.WithDeadline(s.ctx, deadline)
	defer cancel()

	err = s.adapter.Send(ctx, &internal.Stream{
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
		select {
		case <-s.ctx.Done():
			err = s.Err()
		default:
			err = psrpc.ErrRequestTimedOut
		}
	}

	return
}

func (s *stream[SendType, RecvType]) Hijack() {
	s.mu.Lock()
	s.hijacked = true
	s.mu.Unlock()
}

func (s *stream[SendType, RecvType]) Hijacked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hijacked
}

func (s *stream[RequestType, ResponseType]) Close(cause error) error {
	return s.handler.Close(cause)
}

func (s *streamBase[RequestType, ResponseType]) Close(cause error) error {
	if cause == nil {
		cause = psrpc.ErrStreamClosed
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return s.err
	}

	s.closed = true
	s.err = cause
	s.mu.Unlock()

	msg := &internal.StreamClose{}
	var e psrpc.Error
	if errors.As(cause, &e) {
		msg.Error = e.Error()
		msg.Code = string(e.Code())
	} else {
		msg.Error = cause.Error()
		msg.Code = string(psrpc.Unknown)
	}

	now := time.Now()
	err := s.adapter.Send(context.Background(), &internal.Stream{
		StreamId:  s.streamID,
		RequestId: rand.NewRequestID(),
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(s.Timeout).UnixNano(),
		Body: &internal.Stream_Close{
			Close: msg,
		},
	})

	s.pending.Wait()
	s.adapter.Close(s.streamID)
	s.cancel()
	close(s.recvChan)

	return err
}

func (s *streamBase[SendType, RecvType]) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}
