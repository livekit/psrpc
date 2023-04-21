package psrpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
)

const DefaultServerTimeout = time.Second * 3

type ServerOption func(*ServerOpts)

type ServerOpts struct {
	Timeout            time.Duration
	ChannelSize        int
	Interceptors       []ServerRPCInterceptor
	StreamInterceptors []StreamInterceptor
	ChainedInterceptor ServerRPCInterceptor
}

func WithServerTimeout(timeout time.Duration) ServerOption {
	return func(o *ServerOpts) {
		o.Timeout = timeout
	}
}

func WithServerChannelSize(size int) ServerOption {
	return func(o *ServerOpts) {
		if size > 0 {
			o.ChannelSize = size
		}
	}
}

// Server interceptors wrap the service implementation
type ServerRPCInterceptor func(ctx context.Context, req proto.Message, info RPCInfo, handler ServerRPCHandler) (proto.Message, error)
type ServerRPCHandler func(context.Context, proto.Message) (proto.Message, error)

func WithServerRPCInterceptors(interceptors ...ServerRPCInterceptor) ServerOption {
	return func(o *ServerOpts) {
		for _, interceptor := range interceptors {
			if interceptor != nil {
				o.Interceptors = append(o.Interceptors, interceptor)
			}
		}
	}
}

func WithServerStreamInterceptors(interceptors ...StreamInterceptor) ServerOption {
	return func(o *ServerOpts) {
		o.StreamInterceptors = append(o.StreamInterceptors, interceptors...)
	}
}

func WithServerOptions(opts ...ServerOption) ServerOption {
	return func(o *ServerOpts) {
		for _, opt := range opts {
			opt(o)
		}
	}
}
