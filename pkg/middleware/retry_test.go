package middleware

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

func TestIsTimeout(t *testing.T) {
	var cases = []struct {
		Name string
		Err  error
		Exp  bool
	}{
		{Name: "unknown", Err: errors.New("test"), Exp: true},

		{Name: "psrpc timeout", Err: psrpc.NewErrorf(psrpc.DeadlineExceeded, "test"), Exp: true},
		{Name: "psrpc unavailable", Err: psrpc.NewErrorf(psrpc.Unavailable, "test"), Exp: true},
		{Name: "psrpc internal", Err: psrpc.NewErrorf(psrpc.Internal, "test"), Exp: false},
		{Name: "psrpc unknown", Err: psrpc.NewErrorf(psrpc.Unknown, "test"), Exp: false},

		{Name: "grpc timeout", Err: status.Error(codes.DeadlineExceeded, "test"), Exp: true},
		{Name: "grpc unavailable", Err: status.Error(codes.Unavailable, "test"), Exp: true},
		{Name: "grpc internal", Err: status.Error(codes.Internal, "test"), Exp: false},
		{Name: "grpc unknown", Err: status.Error(codes.Unknown, "test"), Exp: false},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			require.Equal(t, c.Exp, isTimeout(c.Err))
			require.Equal(t, c.Exp, isTimeout(fmt.Errorf("wrapped: %w", c.Err)))
		})
	}
}

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

	t.Run("TestSuccess", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return true }
		ri := NewRPCRetryInterceptor(ro)

		errs := []error{nil}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil)

		require.Equal(t, 1, len(timeouts))
		require.Equal(t, ro.Timeout, timeouts[0])
	})

	t.Run("TestFailureAllErrorsRetryable", func(t *testing.T) {
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

	t.Run("TestFailureNoErrorRetryable", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return false }
		ri := NewRPCRetryInterceptor(ro)

		errs := []error{errors.New("test error")}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil)

		require.Equal(t, 1, len(timeouts))
		require.Equal(t, ro.Timeout, timeouts[0])
	})

	t.Run("TestCustomParameters", func(t *testing.T) {
		lastTry := time.Now()

		ro.GetRetryParameters = func(err error, attempt int) (retry bool, timeout time.Duration, waitTime time.Duration) {
			if attempt > 1 {
				now := time.Now()
				require.InDelta(t, 500*time.Millisecond, now.Sub(lastTry), float64(20*time.Millisecond), "Retry didn't wait for required interval")
				lastTry = now
			}

			if attempt == 3 {
				return false, 0, 0
			}

			return true, 100 * time.Millisecond, 500 * time.Millisecond
		}

		ri := NewRPCRetryInterceptor(ro)

		errs := make([]error, 3)
		for i, _ := range errs {
			errs[i] = errors.New("test error")
		}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil)

		require.Equal(t, ro.MaxAttempts, len(timeouts))

		for _, timeout := range timeouts {
			require.Equal(t, 100*time.Millisecond, timeout)
		}
	})
}
