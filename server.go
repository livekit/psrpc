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

import (
	"context"
	"time"

	"google.golang.org/protobuf/proto"
)

const DefaultServerTimeout = time.Second * 3

type ServerOption func(*ServerOpts)

type ServerOpts struct {
	ServerID           string
	Timeout            time.Duration
	ChannelSize        int
	Interceptors       []ServerRPCInterceptor
	StreamInterceptors []StreamInterceptor
	ChainedInterceptor ServerRPCInterceptor
	RequestObserver    RequestObserver
}

func WithServerID(id string) ServerOption {
	return func(o *ServerOpts) {
		o.ServerID = id
	}
}

func WithServerTimeout(timeout time.Duration) ServerOption {
	return func(o *ServerOpts) {
		o.Timeout = timeout
	}
}

func WithServerChannelSize(size int) ServerOption {
	return func(o *ServerOpts) {
		if size > 0 {
			o.ChannelSize = size
		}
	}
}

// Server interceptors wrap the service implementation
type ServerRPCInterceptor func(ctx context.Context, req proto.Message, info RPCInfo, handler ServerRPCHandler) (proto.Message, error)
type ServerRPCHandler func(context.Context, proto.Message) (proto.Message, error)

func WithServerRPCInterceptors(interceptors ...ServerRPCInterceptor) ServerOption {
	return func(o *ServerOpts) {
		for _, interceptor := range interceptors {
			if interceptor != nil {
				o.Interceptors = append(o.Interceptors, interceptor)
			}
		}
	}
}

func WithServerStreamInterceptors(interceptors ...StreamInterceptor) ServerOption {
	return func(o *ServerOpts) {
		o.StreamInterceptors = append(o.StreamInterceptors, interceptors...)
	}
}

func WithServerOptions(opts ...ServerOption) ServerOption {
	return func(o *ServerOpts) {
		for _, opt := range opts {
			opt(o)
		}
	}
}

// ClaimOutcome is the result of the claim negotiation for a single request.
type ClaimOutcome int

const (
	// ClaimGranted means this server won the claim and ran the handler.
	ClaimGranted ClaimOutcome = iota
	// ClaimLostToPeer means another server won the claim. Expected on
	// broadcast RPCs; on a queue RPC it means more than one member received
	// the same request, which is worth knowing about.
	ClaimLostToPeer
	// ClaimAbandoned means this server bid and the client never answered.
	// The client has already returned ErrNoResponse to its caller, so
	// without this event the request leaves no trace anywhere.
	ClaimAbandoned
)

func (o ClaimOutcome) String() string {
	switch o {
	case ClaimGranted:
		return "granted"
	case ClaimLostToPeer:
		return "lost_to_peer"
	case ClaimAbandoned:
		return "abandoned"
	default:
		return "invalid"
	}
}

// RequestObserver receives server-side request lifecycle events that occur
// outside the interceptor chain. Interceptors wrap only the handler, so they
// cannot observe a request that is dropped before dispatch or whose claim is
// never granted -- precisely the cases where the client reports a failure and
// the server records nothing.
//
// Implementations must be non-blocking; they are called from the request read
// loop and from claim negotiation.
type RequestObserver interface {
	// OnRequestReceived fires once per request read off the bus, before the
	// expiry check and before dispatch.
	OnRequestReceived(info RPCInfo)
	// OnRequestExpired fires when a request is discarded because it arrived
	// after its expiry. The handler is not invoked.
	OnRequestExpired(info RPCInfo, lateBy time.Duration)
	// OnClaim fires once the claim negotiation settles, with the time spent
	// waiting for the client's decision.
	OnClaim(info RPCInfo, outcome ClaimOutcome, wait time.Duration)
}

// WithServerObserver installs a RequestObserver for lifecycle events that the
// interceptor chain cannot see.
func WithServerObserver(observer RequestObserver) ServerOption {
	return func(o *ServerOpts) {
		o.RequestObserver = observer
	}
}
