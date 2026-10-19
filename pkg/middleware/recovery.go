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
	"fmt"
	"runtime/debug"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/logger"
)

// Recover from server panics. Should always be the last interceptor
func WithServerRecovery() psrpc.ServerRPCInterceptor {
	return func(ctx context.Context, req proto.Message, _ psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (resp proto.Message, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error(fmt.Errorf("panic: %v\n%s", r, debug.Stack()), "server handler panic")
				err = psrpc.NewErrorf(psrpc.Internal, "server handler panic")
			}
		}()

		resp, err = handler(ctx, req)
		return
	}
}
