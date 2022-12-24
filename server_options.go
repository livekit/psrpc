package psrpc

import (
	"time"
)

const (
	DefaultServerTimeout = time.Second * 3
)

type ServerOption func(*serverOpts)

type serverOpts struct {
	timeout                  time.Duration
	channelSize              int
	unaryInterceptors        []UnaryServerInterceptor
	chainedUnaryInterceptors UnaryServerInterceptor
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

func WithUnaryServerInterceptors(interceptors ...UnaryServerInterceptor) ServerOption {
	return func(o *serverOpts) {
		for _, interceptor := range interceptors {
			if interceptor != nil {
				o.unaryInterceptors = append(o.unaryInterceptors, interceptor)
			}
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

	o.chainedUnaryInterceptors = chainUnaryInterceptors(o.unaryInterceptors)
	return *o
}
