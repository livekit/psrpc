package psrpc

import (
	"time"
)

const (
	DefaultServerTimeout = time.Second * 3
)

type ServerOption func(*serverOpts)

type serverOpts struct {
	timeout            time.Duration
	channelSize        int
	interceptors       []ServerInterceptor
	streamInterceptors []StreamInterceptorFactory
	chainedInterceptor ServerInterceptor
}

func WithServerTimeout(timeout time.Duration) ServerOption {
	return func(o *serverOpts) {
		o.timeout = timeout
	}
}

func WithServerChannelSize(size int) ServerOption {
	return func(o *serverOpts) {
		if size > 0 {
			o.channelSize = size
		}
	}
}

func WithServerInterceptors(interceptors ...ServerInterceptor) ServerOption {
	return func(o *serverOpts) {
		for _, interceptor := range interceptors {
			if interceptor != nil {
				o.interceptors = append(o.interceptors, interceptor)
			}
		}
	}
}

func WithServerStreamInterceptors(interceptors ...StreamInterceptorFactory) ServerOption {
	return func(o *serverOpts) {
		o.streamInterceptors = append(o.streamInterceptors, interceptors...)
	}
}

func WithServerOptions(opts ...ServerOption) ServerOption {
	return func(o *serverOpts) {
		for _, opt := range opts {
			opt(o)
		}
	}
}

func getServerOpts(opts ...ServerOption) serverOpts {
	o := &serverOpts{
		timeout:     DefaultServerTimeout,
		channelSize: DefaultChannelSize,
	}
	for _, opt := range opts {
		opt(o)
	}

	o.chainedInterceptor = chainServerInterceptors(o.interceptors)
	return *o
}
