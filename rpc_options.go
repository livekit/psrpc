package psrpc

import (
	"time"
)

const (
	DefaultTimeout = time.Second * 3
	ChannelSize    = 100
)

// Server options

type ServerOpt func(serverOpts) serverOpts

type serverOpts struct {
	timeout time.Duration
}

func WithServerTimeout(timeout time.Duration) ServerOpt {
	return func(o serverOpts) serverOpts {
		o.timeout = timeout
		return o
	}
}

func getServerOpts(opts ...ServerOpt) serverOpts {
	options := serverOpts{
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}

// Client options

type ClientOpt func(clientOpts) clientOpts

type clientOpts struct {
	timeout time.Duration
}

func WithClientTimeout(timeout time.Duration) ClientOpt {
	return func(o clientOpts) clientOpts {
		o.timeout = timeout
		return o
	}
}

func getClientOpts(opts ...ClientOpt) clientOpts {
	options := clientOpts{
		timeout: DefaultTimeout,
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}

// Request options

type RequestOpt func(reqOpts) reqOpts

type reqOpts struct {
	timeout       time.Duration
	selectionOpts SelectionOpts
}

type SelectionOpts struct {
	MinimumAffinity      float32       // minimum affinity for a server to be considered a valid handler
	AcceptFirstAvailable bool          // go fast
	AffinityTimeout      time.Duration // server selection deadline
	ShortCircuitTimeout  time.Duration // deadline imposed after receiving first response
}

func WithSelectionOpts(opts SelectionOpts) RequestOpt {
	return func(o reqOpts) reqOpts {
		o.selectionOpts = opts
		return o
	}
}

func WithRequestTimeout(timeout time.Duration) RequestOpt {
	return func(o reqOpts) reqOpts {
		o.timeout = timeout
		return o
	}
}

func getRequestOpts(o clientOpts, opts ...RequestOpt) reqOpts {
	options := reqOpts{
		timeout: o.timeout,
		selectionOpts: SelectionOpts{
			AcceptFirstAvailable: true,
		},
	}
	for _, opt := range opts {
		options = opt(options)
	}
	return options
}
