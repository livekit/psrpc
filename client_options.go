package psrpc

import "time"

const (
	DefaultClientTimeout = time.Second * 3
)

type ClientOption func(*clientOpts)

type clientOpts struct {
	timeout     time.Duration
	channelSize int
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
