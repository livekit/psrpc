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

const InternalErrorMessage = "internal error"

type Logger interface {
	Warnw(msg string, err error, keysAndValues ...any)
}

// WithServerErrorMasking replaces errors with no status code or an Unknown code
// with an opaque internal error. grpc-go otherwise serializes an uncoded error as
// codes.Unknown carrying the full error string, which may leak internal details to the caller.
// Must be the first interceptor in the chain (outermost), such that the masking logic is
// applied to the final error
func WithServerErrorMasking(l Logger) psrpc.ServerRPCInterceptor {
	return func(ctx context.Context, req proto.Message, info psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
		res, err := handler(ctx, req)
		if !needsMasking(err) {
			return res, err
		}

		l.Warnw("masked an unhandled error returned from rpc", err, "rpc", info.Service+"/"+info.Method)

		// The response is passed through untouched: this interceptor's job is to replace
		// the error and nothing else. psrpc ignores the response whenever the error is
		// non-nil, and res can only be whatever the handler itself returned
		return res, psrpc.NewErrorf(psrpc.Internal, InternalErrorMessage)
	}
}

// needsMasking returns true if the error does not have a status code in its chain,
// or if it's unknown. Unknown counts as "no code" because that is what an unclassified
// error becomes once it has crossed a psrpc or gRPC boundary, so masking on it also
// covers errors this service is relaying from another one
func needsMasking(err error) bool {
	if err == nil {
		return false
	}
	code, ok := psrpc.GetErrorCode(err)
	return !ok || code == psrpc.Unknown
}
