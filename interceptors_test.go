package psrpc

import (
	"context"
	"fmt"
	"testing"

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
