package server

import (
	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/interceptors"
)

func getServerOpts(opts ...psrpc.ServerOption) psrpc.ServerOpts {
	o := &psrpc.ServerOpts{
		Timeout:     psrpc.DefaultServerTimeout,
		ChannelSize: bus.DefaultChannelSize,
	}
	for _, opt := range opts {
		opt(o)
	}

	o.ChainedInterceptor = interceptors.ChainServerInterceptors(o.Interceptors)
	return *o
}
