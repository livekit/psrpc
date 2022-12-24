package psrpc

import "time"

type RequestOption func(*reqOpts)

type SelectionOpts struct {
	MinimumAffinity      float32       // minimum affinity for a server to be considered a valid handler
	AcceptFirstAvailable bool          // go fast
	AffinityTimeout      time.Duration // server selection deadline
	ShortCircuitTimeout  time.Duration // deadline imposed after receiving first response
}

func WithSelectionOpts(opts SelectionOpts) RequestOption {
	return func(o *reqOpts) {
		o.selectionOpts = opts
	}
}

func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(o *reqOpts) {
		o.timeout = timeout
	}
}

type reqOpts struct {
	timeout       time.Duration
	selectionOpts SelectionOpts
}

func getRequestOpts(options clientOpts, opts ...RequestOption) reqOpts {
	o := &reqOpts{
		timeout: options.timeout,
		selectionOpts: SelectionOpts{
			AcceptFirstAvailable: true,
		},
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}
