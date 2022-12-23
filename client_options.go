package psrpc

import "time"

const (
	DefaultClientTimeout = time.Second * 3
)

type ClientOption func(*clientOpts)

func WithClientTimeout(timeout time.Duration) ClientOption {
	return func(o *clientOpts) {
		o.timeout = timeout
	}
}

type clientOpts struct {
	timeout time.Duration
}

func getClientOpts(opts ...ClientOption) clientOpts {
	o := &clientOpts{
		timeout: DefaultClientTimeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
