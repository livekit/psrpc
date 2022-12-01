package psrpc

import (
	"time"
)

type rpcOpts struct {
	channelSize    int
	requestTimeout time.Duration
}

type RPCOption func(rpcOpts) rpcOpts

func getOpts(opts ...RPCOption) rpcOpts {
	options := rpcOpts{
		channelSize:    100,
		requestTimeout: time.Second * 3,
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}

func WithChannelSize(size int) RPCOption {
	return func(o rpcOpts) rpcOpts {
		o.channelSize = size
		return o
	}
}

func WithTimeout(timeout time.Duration) RPCOption {
	return func(o rpcOpts) rpcOpts {
		o.requestTimeout = timeout
		return o
	}
}
