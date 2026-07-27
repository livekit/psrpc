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
	// ClaimGranted: this server won the claim; the handler runs.
	ClaimGranted ClaimOutcome = iota
	// ClaimLostToPeer: another server won. Expected on broadcast RPCs; on a
	// queue RPC it implies more than one member received the request.
	ClaimLostToPeer
	// ClaimTimedOut: this server bid and the claim expired ungranted. The
	// handler does not run, so the request is safe to retry.
	ClaimTimedOut
)

func (o ClaimOutcome) String() string {
	switch o {
	case ClaimGranted:
		return "granted"
	case ClaimLostToPeer:
		return "lost_to_peer"
	case ClaimTimedOut:
		return "timed_out"
	default:
		return "invalid"
	}
}

// RequestObserver receives server-side lifecycle events for requests that
// never reach the handler, and so are invisible to ServerRPCInterceptor.
//
// Implementations must not block: OnRequestReceived and OnRequestExpired are
// called on the request read loop, OnClaim on the claiming goroutine.
type RequestObserver interface {
	// OnRequestReceived fires once per request read off the bus, before the
	// expiry check and before dispatch.
	OnRequestReceived(info RPCInfo)
	// OnRequestExpired fires when a request is read after its expiry. The
	// handler is not invoked; lateBy is the interval past expiry.
	OnRequestExpired(info RPCInfo, lateBy time.Duration)
	// OnClaim fires once the claim negotiation settles, with the time spent
	// waiting for the client's decision.
	OnClaim(info RPCInfo, outcome ClaimOutcome, wait time.Duration)
}

// WithServerObserver installs a RequestObserver. Nil is the default and
// disables all lifecycle events.
func WithServerObserver(observer RequestObserver) ServerOption {
	return func(o *ServerOpts) {
		o.RequestObserver = observer
	}
}
