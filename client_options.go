package psrpc

import (
	"time"
)

const (
	DefaultClientTimeout = time.Second * 3
)

type ClientOption func(*clientOpts)

type clientOpts struct {
	timeout              time.Duration
	channelSize          int
	enableStreams        bool
	requestHooks         []ClientRequestHook
	responseHooks        []ClientResponseHook
	rpcInterceptors      []RPCInterceptorFactory
	multiRPCInterceptors []MultiRPCInterceptorFactory
	streamInterceptors   []StreamInterceptorFactory
}

func WithClientTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOpts) {
		o.timeout = timeout
	}
}

func WithClientChannelSize(size int) ClientOption {
	return func(o *clientOpts) {
		o.channelSize = size
	}
}

func WithClientRequestHooks(hooks ...ClientRequestHook) ClientOption {
	return func(o *clientOpts) {
		o.requestHooks = append(o.requestHooks, hooks...)
	}
}

func WithClientResponseHooks(hooks ...ClientResponseHook) ClientOption {
	return func(o *clientOpts) {
		o.responseHooks = append(o.responseHooks, hooks...)
	}
}

func WithClientRPCInterceptors(interceptors ...RPCInterceptorFactory) ClientOption {
	return func(o *clientOpts) {
		o.rpcInterceptors = append(o.rpcInterceptors, interceptors...)
	}
}

func WithClientMultiRPCInterceptors(interceptors ...MultiRPCInterceptorFactory) ClientOption {
	return func(o *clientOpts) {
		o.multiRPCInterceptors = append(o.multiRPCInterceptors, interceptors...)
	}
}

func WithClientStreamInterceptors(interceptors ...StreamInterceptorFactory) ClientOption {
	return func(o *clientOpts) {
		o.streamInterceptors = append(o.streamInterceptors, interceptors...)
	}
}

func WithClientOptions(opts ...ClientOption) ClientOption {
	return func(o *clientOpts) {
		for _, opt := range opts {
			opt(o)
		}
	}
}

func withStreams() ClientOption {
	return func(o *clientOpts) {
		o.enableStreams = true
	}
}

func getClientOpts(opts ...ClientOption) clientOpts {
	o := &clientOpts{
		timeout:     DefaultClientTimeout,
		channelSize: DefaultChannelSize,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
