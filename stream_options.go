package psrpc

import "time"

type StreamOption func(*streamOpts)

func WithTimeout(timeout time.Duration) StreamOption {
	return func(o *streamOpts) {
		o.timeout = timeout
	}
}

type streamOpts struct {
	timeout time.Duration
}

func getStreamOpts(options streamOpts, opts ...StreamOption) streamOpts {
	o := &streamOpts{
		timeout: options.timeout,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
