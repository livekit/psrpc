package psrpc

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
)

const (
	DefaultClientTimeout        = time.Second * 3
	DefaultAffinityTimeout      = time.Second
	DefaultAffinityShortCircuit = time.Millisecond * 200
)

type ClientOption func(*ClientOpts)

type ClientOpts struct {
	Timeout              time.Duration
	ChannelSize          int
	EnableStreams        bool
	RequestHooks         []ClientRequestHook
	ResponseHooks        []ClientResponseHook
	RpcInterceptors      []ClientRPCInterceptor
	MultiRPCInterceptors []ClientMultiRPCInterceptor
	StreamInterceptors   []StreamInterceptor
}

func WithClientTimeout(timeout time.Duration) ClientOption {
	return func(o *ClientOpts) {
		o.Timeout = timeout
	}
}

func WithClientChannelSize(size int) ClientOption {
	return func(o *ClientOpts) {
		o.ChannelSize = size
	}
}

// Request hooks are called as soon as the request is made
type ClientRequestHook func(ctx context.Context, req proto.Message, info RPCInfo)

func WithClientRequestHooks(hooks ...ClientRequestHook) ClientOption {
	return func(o *ClientOpts) {
		o.RequestHooks = append(o.RequestHooks, hooks...)
	}
}

// Response hooks are called just before responses are returned
// For multi-requests, response hooks are called on every response, and block while executing
type ClientResponseHook func(ctx context.Context, req proto.Message, info RPCInfo, res proto.Message, err error)

func WithClientResponseHooks(hooks ...ClientResponseHook) ClientOption {
	return func(o *ClientOpts) {
		o.ResponseHooks = append(o.ResponseHooks, hooks...)
	}
}

type ClientRPCInterceptor func(info RPCInfo, next ClientRPCHandler) ClientRPCHandler
type ClientRPCHandler func(ctx context.Context, req proto.Message, opts ...RequestOption) (proto.Message, error)

func WithClientRPCInterceptors(interceptors ...ClientRPCInterceptor) ClientOption {
	return func(o *ClientOpts) {
		o.RpcInterceptors = append(o.RpcInterceptors, interceptors...)
	}
}

type ClientMultiRPCInterceptor func(info RPCInfo, next ClientMultiRPCHandler) ClientMultiRPCHandler
type ClientMultiRPCHandler interface {
	Send(ctx context.Context, msg proto.Message, opts ...RequestOption) error
	Recv(msg proto.Message, err error)
	Close()
}

func WithClientMultiRPCInterceptors(interceptors ...ClientMultiRPCInterceptor) ClientOption {
	return func(o *ClientOpts) {
		o.MultiRPCInterceptors = append(o.MultiRPCInterceptors, interceptors...)
	}
}

func WithClientStreamInterceptors(interceptors ...StreamInterceptor) ClientOption {
	return func(o *ClientOpts) {
		o.StreamInterceptors = append(o.StreamInterceptors, interceptors...)
	}
}

func WithClientOptions(opts ...ClientOption) ClientOption {
	return func(o *ClientOpts) {
		for _, opt := range opts {
			opt(o)
		}
	}
}
