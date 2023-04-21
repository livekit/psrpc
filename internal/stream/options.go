package stream

import (
	"github.com/livekit/psrpc"
)

func getStreamOpts(options psrpc.StreamOpts, opts ...psrpc.StreamOption) psrpc.StreamOpts {
	o := &psrpc.StreamOpts{
		Timeout: options.Timeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
