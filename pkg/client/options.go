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

package client

import (
	"context"
	"fmt"
	"time"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/info"
)

func withStreams() psrpc.ClientOption {
	return func(o *psrpc.ClientOpts) {
		o.EnableStreams = true
	}
}

func getClientOpts(opts ...psrpc.ClientOption) psrpc.ClientOpts {
	o := &psrpc.ClientOpts{
		SelectionTimeout: psrpc.DefaultAffinityTimeout,
		ChannelSize:      bus.DefaultChannelSize,
	}
	for _, opt := range opts {
		opt(o)
	}
	return *o
}

func getRequestOpts(ctx context.Context, i *info.RequestInfo, options psrpc.ClientOpts, opts ...psrpc.RequestOption) psrpc.RequestOpts {
	o := &psrpc.RequestOpts{
		Timeout: options.Timeout,
	}
	if deadline, ok := ctx.Deadline(); ok {
		if dt := time.Until(deadline); o.Timeout == 0 || o.Timeout > dt {
			o.Timeout = dt
		}
	}
	if o.Timeout == 0 {
		o.Timeout = psrpc.DefaultClientTimeout
	}
	if i.AffinityEnabled {
		o.SelectionOpts = psrpc.SelectionOpts{
			AffinityTimeout:     options.SelectionTimeout,
			ShortCircuitTimeout: psrpc.DefaultAffinityShortCircuit,
		}
	} else {
		o.SelectionOpts = psrpc.SelectionOpts{
			AffinityTimeout:      options.SelectionTimeout,
			AcceptFirstAvailable: true,
		}
	}

	for _, opt := range opts {
		opt(o)
	}

	return *o
}

func getRequestInterceptors[T psrpc.RequestInterceptor](base []T, as []any) []T {
	if as == nil {
		return base
	}

	c := make([]T, len(base), len(base)+len(as))
	copy(c, base)
	for _, a := range as {
		i, ok := a.(T)
		if !ok {
			var ex T
			panic(fmt.Sprintf("cannot use %T as %T in psrpc request", a, ex))
		}
		c = append(c, i)
	}
	return c
}
