package psrpc

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/livekit/psrpc/internal"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestInterceptors(t *testing.T) {
	s := ""

	createInterceptor := func(i int) ServerInterceptor {
		return func(ctx context.Context, req proto.Message, _ RPCInfo, handler Handler) (proto.Message, error) {
			s += fmt.Sprint(i)
			res, err := handler(ctx, req)
			s += fmt.Sprint(i)
			return res, err
		}
	}

	var code ErrorCode
	serverErrorLogger := func(ctx context.Context, req proto.Message, _ RPCInfo, handler Handler) (proto.Message, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			code = Unknown
			if e, ok := err.(Error); ok {
				code = e.Code()
			}
		}
		return resp, err
	}

	interceptors := []ServerInterceptor{
		serverErrorLogger,
		createInterceptor(1),
		createInterceptor(2),
		createInterceptor(3),
		WithServerRecovery(),
	}
	chained := chainServerInterceptors(interceptors)
	svcImpl := func(ctx context.Context, _ proto.Message) (proto.Message, error) {
		s += fmt.Sprint(4)
		panic("panic")
	}

	handler := func(ctx context.Context, req proto.Message) (proto.Message, error) {
		info := RPCInfo{Method: "myRPC"}
		res, err := chained(ctx, req, info, func(context.Context, proto.Message) (proto.Message, error) {
			return svcImpl(ctx, req)
		})
		return res, err
	}

	_, err := handler(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, "1234321", s)
	require.Equal(t, Internal, code)
}

type testContextKey string

func TestRPCInterceptors(t *testing.T) {
	s := ""

	newTestFactory := func(k string) RPCInterceptorFactory {
		return func(info RPCInfo, next RPCInterceptor) RPCInterceptor {
			return func(ctx context.Context, req proto.Message, opts ...RequestOption) (res proto.Message, err error) {
				ctx = context.WithValue(ctx, testContextKey(k), true)
				opts = append(opts, WithRequestTimeout(0))
				s += k
				res, err = next(ctx, req, opts...)
				s += k
				return
			}
		}
	}

	root := func(ctx context.Context, req proto.Message, opts ...RequestOption) (res proto.Message, err error) {
		require.Equal(t, true, ctx.Value(testContextKey("1")).(bool))
		require.Equal(t, true, ctx.Value(testContextKey("2")).(bool))
		require.Equal(t, true, ctx.Value(testContextKey("3")).(bool))
		require.Equal(t, 3, len(opts))
		s += "4"
		return nil, ErrNoResponse
	}

	interceptors := []RPCInterceptorFactory{
		newTestFactory("1"),
		newTestFactory("2"),
		newTestFactory("3"),
	}
	_, err := chainClientInterceptors[RPCInterceptor](interceptors, RPCInfo{}, root)(context.Background(), &internal.Request{})
	require.Equal(t, "1234321", s)
	require.Equal(t, ErrNoResponse, err)
}

type testMultiRPCInterceptor struct {
	MultiRPCInterceptor
	sendFunc func(ctx context.Context, msg proto.Message, opts ...RequestOption) error
	recvFunc func(msg proto.Message, err error)
}

func (t *testMultiRPCInterceptor) Send(ctx context.Context, msg proto.Message, opts ...RequestOption) (err error) {
	return t.sendFunc(ctx, msg, opts...)
}

func (t *testMultiRPCInterceptor) Recv(msg proto.Message, err error) {
	t.recvFunc(msg, err)
}

func TestMultiRPCInterceptors(t *testing.T) {
	s := ""

	newTestFactory := func(k string) MultiRPCInterceptorFactory {
		return func(info RPCInfo, next MultiRPCInterceptor) MultiRPCInterceptor {
			return &testMultiRPCInterceptor{
				sendFunc: func(ctx context.Context, msg proto.Message, opts ...RequestOption) error {
					ctx = context.WithValue(ctx, testContextKey(k), true)
					opts = append(opts, WithRequestTimeout(0))
					s += k
					return next.Send(ctx, msg, opts...)
				},
				recvFunc: func(msg proto.Message, err error) {
					next.Recv(msg, err)
					s += k
				},
			}
		}
	}

	ok := errors.New("ok")

	root := &testMultiRPCInterceptor{
		sendFunc: func(ctx context.Context, msg proto.Message, opts ...RequestOption) error {
			require.Equal(t, true, ctx.Value(testContextKey("1")).(bool))
			require.Equal(t, true, ctx.Value(testContextKey("2")).(bool))
			require.Equal(t, true, ctx.Value(testContextKey("3")).(bool))
			require.Equal(t, 3, len(opts))
			return ok
		},
		recvFunc: func(msg proto.Message, err error) {
			s += "4"
		},
	}

	interceptors := []MultiRPCInterceptorFactory{
		newTestFactory("1"),
		newTestFactory("2"),
		newTestFactory("3"),
	}
	interceptor := chainClientInterceptors[MultiRPCInterceptor](interceptors, RPCInfo{}, root)
	err := interceptor.Send(context.Background(), &internal.Request{})
	interceptor.Recv(nil, nil)
	require.Equal(t, "1234321", s)
	require.Equal(t, ok, err)
}
