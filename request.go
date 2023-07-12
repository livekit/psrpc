package psrpc

import "time"

type RequestOption func(*RequestOpts)

type RequestOpts struct {
	Timeout       time.Duration
	SelectionOpts SelectionOpts
}

func WithRequestTimeout(timeout time.Duration) RequestOption {
	return func(o *RequestOpts) {
		o.Timeout = timeout
	}
}

type SelectionOpts struct {
	MinimumAffinity      float32       // minimum affinity for a server to be considered a valid handler
	MaximumAffinity      float32       // if > 0, any server returning a max score will be selected immediately
	AcceptFirstAvailable bool          // go fast
	AffinityTimeout      time.Duration // server selection deadline
	ShortCircuitTimeout  time.Duration // deadline imposed after receiving first response
}

func WithSelectionOpts(opts SelectionOpts) RequestOption {
	return func(o *RequestOpts) {
		o.SelectionOpts = opts
	}
}
