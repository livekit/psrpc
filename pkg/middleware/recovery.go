package middleware

import (
	"context"
	"runtime/debug"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

// Recover from server panics. Should always be the last interceptor
func WithServerRecovery() psrpc.ServerRPCInterceptor {
	return func(ctx context.Context, req proto.Message, _ psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (resp proto.Message, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = psrpc.NewErrorf(psrpc.Internal, "Caught server panic. Stack trace:\n%s", string(debug.Stack()))
			}
		}()

		resp, err = handler(ctx, req)
		return
	}
}
