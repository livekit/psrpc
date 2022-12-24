package psrpc

import (
	"context"
	"fmt"
	"runtime/debug"

	"google.golang.org/protobuf/proto"
)

type UnaryServerInterceptor func(ctx context.Context, req proto.Message, handler Handler) (proto.Message, error)

type Handler func(context.Context, proto.Message) (proto.Message, error)

func WithServerRecovery() UnaryServerInterceptor {
	return func(ctx context.Context, req proto.Message, handler Handler) (_ proto.Message, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("Caught server panic. Stack trace:\n%s", string(debug.Stack()))
			}
		}()

		resp, err := handler(ctx, req)
		return resp, err
	}
}

func chainUnaryInterceptors(interceptors []UnaryServerInterceptor) UnaryServerInterceptor {
	filtered := make([]UnaryServerInterceptor, 0, len(interceptors))
	for _, interceptor := range interceptors {
		if interceptor != nil {
			filtered = append(filtered, interceptor)
		}
	}

	switch n := len(filtered); n {
	case 0:
		return nil
	case 1:
		return filtered[0]
	default:
		return func(ctx context.Context, req proto.Message, handler Handler) (proto.Message, error) {
			// the struct ensures the variables are allocated together, rather than separately, since we
			// know they should be garbage collected together. This saves 1 allocation and decreases
			// time/call by about 10% on the microbenchmark.
			var state struct {
				i    int
				next Handler
			}
			state.next = func(ctx context.Context, req proto.Message) (proto.Message, error) {
				if state.i == len(interceptors)-1 {
					return filtered[state.i](ctx, req, handler)
				}
				state.i++
				return filtered[state.i-1](ctx, req, state.next)
			}
			return state.next(ctx, req)
		}
	}

}
