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

package interceptors

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/pkg/info"
)

func ChainClientInterceptors[HandlerType any, InterceptorType ~func(psrpc.RPCInfo, HandlerType) HandlerType](
	interceptors []InterceptorType,
	requestInfo *info.RequestInfo,
	handler HandlerType,
) HandlerType {
	for i := len(interceptors) - 1; i >= 0; i-- {
		handler = interceptors[i](requestInfo.RPCInfo, handler)
	}
	return handler
}

func ChainServerInterceptors(interceptors []psrpc.ServerRPCInterceptor) psrpc.ServerRPCInterceptor {
	switch n := len(interceptors); n {
	case 0:
		return nil
	case 1:
		return interceptors[0]
	default:
		return func(ctx context.Context, req proto.Message, rpcInfo psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
			// the struct ensures the variables are allocated together, rather than separately, since we
			// know they should be garbage collected together. This saves 1 allocation and decreases
			// time/call by about 10% on the microbenchmark.
			var state struct {
				i    int
				next psrpc.ServerRPCHandler
			}
			state.next = func(ctx context.Context, req proto.Message) (proto.Message, error) {
				if state.i == len(interceptors)-1 {
					return interceptors[state.i](ctx, req, rpcInfo, handler)
				}
				state.i++
				return interceptors[state.i-1](ctx, req, rpcInfo, state.next)
			}
			return state.next(ctx, req)
		}
	}
}
