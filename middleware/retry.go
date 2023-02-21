package middleware

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

type RetryOptions struct {
	MaxAttempts int
	Timeout     time.Duration
	Backoff     time.Duration
}

func isPermanentFailure(err error) bool {
	return !(errors.Is(err, psrpc.ErrNoResponse) || errors.Is(err, psrpc.ErrRequestTimedOut))
}

func retry(opt RetryOptions, fn func(timeout time.Duration) error) error {
	attempt := 1
	timeout := opt.Timeout
	for {
		if err := fn(timeout); err == nil || isPermanentFailure(err) || attempt == opt.MaxAttempts {
			return err
		}

		attempt++
		timeout += opt.Backoff
	}
}

func WithRPCRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientRPCInterceptors(NewRetryRPCInterceptorFactory(opt))
}

func NewRetryRPCInterceptorFactory(opt RetryOptions) psrpc.RPCInterceptorFactory {
	return func(info psrpc.RPCInfo, next psrpc.RPCInterceptor) psrpc.RPCInterceptor {
		return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
			retry(opt, func(timeout time.Duration) error {
				nextOpts := make([]psrpc.RequestOption, len(opts)+1)
				copy(nextOpts, opts)
				opts[len(opts)] = psrpc.WithRequestTimeout(timeout)

				res, err = next(ctx, req, nextOpts...)
				return err
			})
			return
		}
	}
}

func WithStreamRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientStreamInterceptors(NewRetryStreamInterceptorFactory(opt))
}

func NewRetryStreamInterceptorFactory(opt RetryOptions) psrpc.StreamInterceptorFactory {
	return func(info psrpc.RPCInfo, next psrpc.StreamInterceptor) psrpc.StreamInterceptor {

		return &streamRetryInterceptor{
			StreamInterceptor: next,
			opt:               opt,
		}
	}
}

type streamRetryInterceptor struct {
	psrpc.StreamInterceptor
	opt RetryOptions
}

func (s *streamRetryInterceptor) Send(msg proto.Message, opts ...psrpc.StreamOption) (err error) {
	return retry(s.opt, func(timeout time.Duration) error {
		nextOpts := make([]psrpc.StreamOption, len(opts)+1)
		copy(nextOpts, opts)
		opts[len(opts)] = psrpc.WithTimeout(timeout)

		return s.StreamInterceptor.Send(msg, nextOpts...)
	})
}
