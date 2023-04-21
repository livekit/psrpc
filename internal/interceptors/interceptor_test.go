package interceptors

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/middleware"
)

func TestInterceptors(t *testing.T) {
	s := ""

	createInterceptor := func(i int) psrpc.ServerRPCInterceptor {
		return func(ctx context.Context, req proto.Message, _ psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
			s += fmt.Sprint(i)
			res, err := handler(ctx, req)
			s += fmt.Sprint(i)
			return res, err
		}
	}

	var code psrpc.ErrorCode
	serverErrorLogger := func(ctx context.Context, req proto.Message, _ psrpc.RPCInfo, handler psrpc.ServerRPCHandler) (proto.Message, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			code = psrpc.Unknown
			if e, ok := err.(psrpc.Error); ok {
				code = e.Code()
			}
		}
		return resp, err
	}

	interceptors := []psrpc.ServerRPCInterceptor{
		serverErrorLogger,
		createInterceptor(1),
		createInterceptor(2),
		createInterceptor(3),
		middleware.WithServerRecovery(),
	}
	chained := ChainServerInterceptors(interceptors)
	svcImpl := func(ctx context.Context, _ proto.Message) (proto.Message, error) {
		s += fmt.Sprint(4)
		panic("panic")
	}

	handler := func(ctx context.Context, req proto.Message) (proto.Message, error) {
		rpcInfo := psrpc.RPCInfo{Method: "myRPC"}
		res, err := chained(ctx, req, rpcInfo, func(context.Context, proto.Message) (proto.Message, error) {
			return svcImpl(ctx, req)
		})
		return res, err
	}

	_, err := handler(context.Background(), nil)
	require.Error(t, err)
	require.Equal(t, "1234321", s)
	require.Equal(t, psrpc.Internal, code)
}

type testContextKey string

func TestRPCInterceptors(t *testing.T) {
	s := ""

	newTestInterceptor := func(k string) psrpc.ClientRPCInterceptor {
		return func(rpcInfo psrpc.RPCInfo, next psrpc.ClientRPCHandler) psrpc.ClientRPCHandler {
			return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
				ctx = context.WithValue(ctx, testContextKey(k), true)
				opts = append(opts, psrpc.WithRequestTimeout(0))
				s += k
				res, err = next(ctx, req, opts...)
				s += k
				return
			}
		}
	}

	handler := func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
		require.Equal(t, true, ctx.Value(testContextKey("1")).(bool))
		require.Equal(t, true, ctx.Value(testContextKey("2")).(bool))
		require.Equal(t, true, ctx.Value(testContextKey("3")).(bool))
		require.Equal(t, 3, len(opts))
		s += "4"
		return nil, psrpc.ErrNoResponse
	}

	interceptors := []psrpc.ClientRPCInterceptor{
		newTestInterceptor("1"),
		newTestInterceptor("2"),
		newTestInterceptor("3"),
	}
	_, err := ChainClientInterceptors[psrpc.ClientRPCHandler](interceptors, &info.RequestInfo{}, handler)(context.Background(), &internal.Request{})
	require.Equal(t, "1234321", s)
	require.Equal(t, psrpc.ErrNoResponse, err)
}

type testMultiRPCInterceptor struct {
	psrpc.ClientMultiRPCHandler
	sendFunc func(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) error
	recvFunc func(msg proto.Message, err error)
}

func (t *testMultiRPCInterceptor) Send(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) (err error) {
	return t.sendFunc(ctx, msg, opts...)
}

func (t *testMultiRPCInterceptor) Recv(msg proto.Message, err error) {
	t.recvFunc(msg, err)
}

func TestMultiRPCInterceptors(t *testing.T) {
	s := ""

	newTestInterceptor := func(k string) psrpc.ClientMultiRPCInterceptor {
		return func(rpcInfo psrpc.RPCInfo, next psrpc.ClientMultiRPCHandler) psrpc.ClientMultiRPCHandler {
			return &testMultiRPCInterceptor{
				sendFunc: func(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) error {
					ctx = context.WithValue(ctx, testContextKey(k), true)
					opts = append(opts, psrpc.WithRequestTimeout(0))
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

	handler := &testMultiRPCInterceptor{
		sendFunc: func(ctx context.Context, msg proto.Message, opts ...psrpc.RequestOption) error {
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

	interceptors := []psrpc.ClientMultiRPCInterceptor{
		newTestInterceptor("1"),
		newTestInterceptor("2"),
		newTestInterceptor("3"),
	}
	interceptor := ChainClientInterceptors[psrpc.ClientMultiRPCHandler](interceptors, &info.RequestInfo{}, handler)
	err := interceptor.Send(context.Background(), &internal.Request{})
	interceptor.Recv(nil, nil)
	require.Equal(t, "1234321", s)
	require.Equal(t, ok, err)
}
