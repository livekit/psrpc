package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/livekit/psrpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestRetryBackoff(t *testing.T) {
	ro := RetryOptions{
		MaxAttempts: 3,
		Timeout:     100 * time.Millisecond,
		Backoff:     200 * time.Millisecond,
	}

	var timeouts []time.Duration
	getClientRpcHandler := func(errors []error) func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
		timeouts = nil
		attempt := 0

		return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
			o := &psrpc.RequestOpts{}

			for _, f := range opts {
				f(o)
			}

			timeouts = append(timeouts, o.Timeout)

			err = errors[attempt]
			attempt++

			return nil, err
		}
	}

	t.Run("Success", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return true }
		ri := NewRPCRetryInterceptor(ro)

		errs := []error{nil}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil)

		require.Equal(t, 1, len(timeouts))
		require.Equal(t, ro.Timeout, timeouts[0])
	})

	t.Run("Failure all errors retryable", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return true }
		ri := NewRPCRetryInterceptor(ro)

		errs := make([]error, 3)
		for i, _ := range errs {
			errs[i] = errors.New("test error")
		}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil)

		require.Equal(t, ro.MaxAttempts, len(timeouts))

		expectedTimeout := ro.Timeout
		for _, timeout := range timeouts {
			require.Equal(t, expectedTimeout, timeout)
			expectedTimeout += ro.Backoff
		}
	})
}
