package psrpc

import (
	"time"

	"google.golang.org/protobuf/proto"
)

type StreamOption func(*StreamOpts)

type StreamOpts struct {
	Timeout time.Duration
}

func WithTimeout(timeout time.Duration) StreamOption {
	return func(o *StreamOpts) {
		o.Timeout = timeout
	}
}

type StreamInterceptor func(info RPCInfo, next StreamHandler) StreamHandler
type StreamHandler interface {
	Recv(msg proto.Message) error
	Send(msg proto.Message, opts ...StreamOption) error
	Close(cause error) error
}
