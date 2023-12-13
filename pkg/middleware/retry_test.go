package middleware

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/livekit/psrpc"
	"google.golang.org/protobuf/proto"
	"gotest.tools/v3/assert"
)

func TestRetryBackoff(t *testing.T) {
	ro := RetryOptions{
		MaxAttempts: 3,
		Timeout:     100 * time.Millisecond,
		Backoff:     200 * time.Millisecond,
	}

	t.Run("Failure, all errors retryable", func(t *testing.T) {
		ro.IsRecoverable = func(err error) bool { return true }
		ri := NewRPCRetryInterceptor(ro)

		var timeouts []time.Duration

		f := func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
			o := &psrpc.RequestOpts{}

			for _, f := range opts {
				f(o)
			}

			timeouts = append(timeouts, o.Timeout)

			return nil, errors.New("Test error")
		}

		h := ri(psrpc.RPCInfo{}, f)
		h(context.Background(), nil)

		assert.Equal(t, ro.MaxAttempts, len(timeouts))

		expectedTimeout = ro.Timeout
		for _, timeout := range timeouts {
			assert.Equal(t, expectedTimeout, timeout)
			expectedTimeout += ro.Backoff
		}
	})
}
