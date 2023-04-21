package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal/bus"
)

type Subscription[MessageType proto.Message] bus.Subscription[MessageType]

type Response[ResponseType proto.Message] struct {
	Result ResponseType
	Err    error
}

type Stream[SendType, RecvType proto.Message] interface {
	Context() context.Context
	Channel() <-chan RecvType
	Send(msg SendType, opts ...StreamOption) error
	Close(cause error) error
	Err() error
}

type ClientStream[SendType, RecvType proto.Message] interface {
	Stream[SendType, RecvType]
}

type ServerStream[SendType, RecvType proto.Message] interface {
	Stream[SendType, RecvType]
	Hijack()
}
