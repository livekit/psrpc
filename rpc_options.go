package psrpc

import (
	"time"
)

const (
	DefaultTimeout = time.Second * 3
	ChannelSize    = 100
)

// RPC Client and Server options

type RPCOption func(rpcOpts) rpcOpts

func WithTimeout(timeout time.Duration) RPCOption {
	return func(o rpcOpts) rpcOpts {
		o.timeout = timeout
		return o
	}
}

type rpcOpts struct {
	timeout time.Duration
}

func getRPCOpts(opts ...RPCOption) rpcOpts {
	options := rpcOpts{
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}

// Handler options

type HandlerOption func(*handler)

func WithAffinityFunc(affinityFunc AffinityFunc) HandlerOption {
	return func(h *handler) {
		h.affinityFunc = affinityFunc
	}
}

// Request options

type RequestOption func(reqOpts) reqOpts

func WithAffinityOpts(opts AffinityOpts) RequestOption {
	return func(o reqOpts) reqOpts {
		o.affinity = opts
		return o
	}
}

func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(o reqOpts) reqOpts {
		o.timeout = timeout
		return o
	}
}

type reqOpts struct {
	timeout  time.Duration
	affinity AffinityOpts
}

func getRequestOpts(o rpcOpts, opts ...RequestOption) reqOpts {
	options := reqOpts{
		timeout: o.timeout,
		affinity: AffinityOpts{
			AcceptFirstAvailable: true,
		},
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}
