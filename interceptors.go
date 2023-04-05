package psrpc

import (
	"context"
	"runtime/debug"

	"google.golang.org/protobuf/proto"
)

// Server interceptors wrap the service implementation
type ServerInterceptor func(ctx context.Context, req proto.Message, info RPCInfo, handler Handler) (proto.Message, error)

type Handler func(context.Context, proto.Message) (proto.Message, error)

// Request hooks are called as soon as the request is made
type ClientRequestHook func(ctx context.Context, req proto.Message, info RPCInfo)

// Response hooks are called just before responses are returned
// For multi-requests, response hooks are called on every response, and block while executing
type ClientResponseHook func(ctx context.Context, req proto.Message, info RPCInfo, resp proto.Message, err error)

type RPCInterceptor func(ctx context.Context, req proto.Message, opts ...RequestOption) (proto.Message, error)

type RPCInterceptorFactory func(info RPCInfo, next RPCInterceptor) RPCInterceptor

type MultiRPCInterceptor interface {
	Send(ctx context.Context, msg proto.Message, opts ...RequestOption) error
	Recv(msg proto.Message, err error)
	Close()
}

type MultiRPCInterceptorFactory func(info RPCInfo, next MultiRPCInterceptor) MultiRPCInterceptor

type StreamInterceptor interface {
	Recv(msg proto.Message) error
	Send(msg proto.Message, opts ...StreamOption) error
	Close(cause error) error
}

type StreamInterceptorFactory func(info RPCInfo, next StreamInterceptor) StreamInterceptor

type RPCInfo struct {
	Service string
	Method  string
	Topic   []string
	Multi   bool
}

// Recover from server panics. Should always be the last interceptor
func WithServerRecovery() ServerInterceptor {
	return func(ctx context.Context, req proto.Message, _ RPCInfo, handler Handler) (resp proto.Message, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = NewErrorf(Internal, "Caught server panic. Stack trace:\n%s", string(debug.Stack()))
			}
		}()

		resp, err = handler(ctx, req)
		return
	}
}

func chainServerInterceptors(interceptors []ServerInterceptor) ServerInterceptor {
	switch n := len(interceptors); n {
	case 0:
		return nil
	case 1:
		return interceptors[0]
	default:
		return func(ctx context.Context, req proto.Message, info RPCInfo, handler Handler) (proto.Message, error) {
			// the struct ensures the variables are allocated together, rather than separately, since we
			// know they should be garbage collected together. This saves 1 allocation and decreases
			// time/call by about 10% on the microbenchmark.
			var state struct {
				i    int
				next Handler
			}
			state.next = func(ctx context.Context, req proto.Message) (proto.Message, error) {
				if state.i == len(interceptors)-1 {
					return interceptors[state.i](ctx, req, info, handler)
				}
				state.i++
				return interceptors[state.i-1](ctx, req, info, state.next)
			}
			return state.next(ctx, req)
		}
	}
}

func chainClientInterceptors[InterceptorType any, FactoryType ~func(RPCInfo, InterceptorType) InterceptorType](factories []FactoryType, info RPCInfo, interceptor InterceptorType) InterceptorType {
	for i := len(factories) - 1; i >= 0; i-- {
		interceptor = factories[i](info, interceptor)
	}
	return interceptor
}
