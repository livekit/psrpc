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

	t.Run("TestCallerTimeoutKept", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return true }
		ri := NewRPCRetryInterceptor(ro)

		errs := make([]error, 3)
		for i := range errs {
			errs[i] = errors.New("test error")
		}
		h := ri(psrpc.RPCInfo{}, getClientRpcHandler(errs))
		h(context.Background(), nil, psrpc.WithRequestTimeout(time.Second))

		require.Equal(t, []time.Duration{time.Second, time.Second, time.Second}, timeouts)
	})

	// a client with a generic retry and a more specific one inside it, like the
	// protocol egress client
	t.Run("TestNestedInnermostTimeoutWins", func(t *testing.T) {
		outer := NewRPCRetryInterceptor(RetryOptions{MaxAttempts: 1, Timeout: 300 * time.Millisecond})
		inner := NewRPCRetryInterceptor(RetryOptions{MaxAttempts: 1, Timeout: 10 * time.Second})

		h := outer(psrpc.RPCInfo{}, inner(psrpc.RPCInfo{}, getClientRpcHandler([]error{nil})))
		h(context.Background(), nil)
		require.Equal(t, []time.Duration{10 * time.Second}, timeouts)

		h = outer(psrpc.RPCInfo{}, inner(psrpc.RPCInfo{}, getClientRpcHandler([]error{nil})))
		h(context.Background(), nil, psrpc.WithRequestTimeout(time.Second))
		require.Equal(t, []time.Duration{time.Second}, timeouts, "the caller's timeout wins over both")
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

// A request no server is subscribed to fails as soon as the broker says so, but
// its retry still waits out the wait it cut short, as it did before.
func TestRetryWaitsOutUnansweredWait(t *testing.T) {
	const wait = 150 * time.Millisecond

	run := func(t *testing.T, first error) time.Duration {
		var starts []time.Time
		h := NewRPCRetryInterceptor(RetryOptions{MaxAttempts: 2})(psrpc.RPCInfo{},
			func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (proto.Message, error) {
				starts = append(starts, time.Now())
				if len(starts) == 1 {
					return nil, first
				}
				return nil, nil
			})
		_, err := h(context.Background(), nil)
		require.NoError(t, err)
		require.Len(t, starts, 2)
		return starts[1].Sub(starts[0])
	}

	t.Run("no subscriber", func(t *testing.T) {
		err := psrpc.NewNoSubscriberError(wait)
		require.ErrorIs(t, err, psrpc.ErrNoResponse, "callers retrying ErrNoResponse must retry this")
		code, ok := psrpc.GetErrorCode(err)
		require.True(t, ok)
		require.Equal(t, psrpc.Unavailable, code)

		require.GreaterOrEqual(t, run(t, err), wait)
	})

	t.Run("other errors", func(t *testing.T) {
		require.Less(t, run(t, psrpc.ErrNoResponse), wait, "only early failures are paced")
	})
}
