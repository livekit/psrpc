package client

import (
	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
)

func withStreams() psrpc.ClientOption {
	return func(o *psrpc.ClientOpts) {
		o.EnableStreams = true
	}
}

func getClientOpts(opts ...psrpc.ClientOption) psrpc.ClientOpts {
	o := &psrpc.ClientOpts{
		Timeout:     psrpc.DefaultClientTimeout,
		ChannelSize: bus.DefaultChannelSize,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}

func getRequestOpts(options psrpc.ClientOpts, opts ...psrpc.RequestOption) psrpc.RequestOpts {
	o := &psrpc.RequestOpts{
		Timeout: options.Timeout,
		SelectionOpts: psrpc.SelectionOpts{
			AcceptFirstAvailable: true,
		},
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
