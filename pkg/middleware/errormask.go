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

package middleware

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

// InternalErrorMessage is the only message a masked error carries.
const InternalErrorMessage = "internal error"

// Logger is the subset of a structured logger WithServerErrorMasking needs. It is
// declared here rather than imported so that psrpc stays free of logging dependencies;
// livekit/protocol's logger.Logger satisfies it as is.
type Logger interface {
	Warnw(msg string, err error, keysAndValues ...any)
}

// WithServerErrorMasking replaces any error a handler returns that was never given a
// deliberate code with an opaque internal error, so that internal detail (database
// driver text, file paths, hostnames) cannot reach the caller. The original error is
// logged as a warning: masking is not meant to lose it, and a warning is the signal that
// a handler has an unclassified error path someone should go and give a code to.
//
// Errors that already carry a code are returned untouched, on the assumption that a code
// was chosen along with a message fit for the caller to read.
//
// Should always be the first interceptor, so that it is outermost and its return value is
// the one that gets serialized.
func WithServerErrorMasking(l Logger) psrpc.ServerRPCInterceptor {
	return func(ctx context.Context, req proto.Message, info psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
		res, err := handler(ctx, req)
		if !needsMasking(err) {
			return res, err
		}

		l.Warnw("masked an unhandled error returned from rpc", err, "rpc", info.Service+"/"+info.Method)

		// The response is passed through untouched: this interceptor's job is to replace
		// the error and nothing else. psrpc ignores the response whenever the error is
		// non-nil, and res can only be whatever the handler itself returned.
		//
		// The masked error deliberately does not wrap its cause. sendResponse resolves the
		// error to serialize by walking the Unwrap chain for a psrpc.Error, so a wrapped
		// cause carrying a code would be found and sent instead of this one.
		return res, psrpc.NewErrorf(psrpc.Internal, InternalErrorMessage)
	}
}

// needsMasking reports whether err was never given a deliberate code.
//
// GetErrorCode reads the code a psrpc.Error was created with, and only falls back to a
// gRPC status for errors from elsewhere. Reading the code back out of GRPCStatus instead
// would run it through ErrorCode.ToGRPC, which maps every code it has no case for to
// codes.Unknown and would therefore mask deliberately coded errors.
//
// Unknown counts as "no code" because that is what an unclassified error becomes in
// transit: a server serializes it as its raw text with code Unknown, and
// NewErrorFromResponse rebuilds it that way on the client. Masking on Unknown is what
// stops an unclassified error leaked by one server from being relayed onward by the next.
func needsMasking(err error) bool {
	if err == nil {
		return false
	}
	code, ok := psrpc.GetErrorCode(err)
	return !ok || code == psrpc.Unknown
}
