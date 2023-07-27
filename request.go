// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
