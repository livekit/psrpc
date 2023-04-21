package middleware

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

type RetryOptions struct {
	MaxAttempts   int
	Timeout       time.Duration
	Backoff       time.Duration
	IsRecoverable func(err error) bool
}

func WithRPCRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientRPCInterceptors(NewRPCRetryInterceptor(opt))
}

func NewRPCRetryInterceptor(opt RetryOptions) psrpc.ClientRPCInterceptor {
	return func(info psrpc.RPCInfo, next psrpc.ClientRPCHandler) psrpc.ClientRPCHandler {
		return func(ctx context.Context, req proto.Message, opts ...psrpc.RequestOption) (res proto.Message, err error) {
			err = retry(opt, ctx.Done(), func(timeout time.Duration) error {
				nextOpts := opts
				if timeout > 0 {
					nextOpts = make([]psrpc.RequestOption, len(opts)+1)
					copy(nextOpts, opts)
					nextOpts[len(opts)] = psrpc.WithRequestTimeout(timeout)
				}

				res, err = next(ctx, req, nextOpts...)
				return err
			})
			return
		}
	}
}

func isTimeout(err error) bool {
	var e psrpc.Error
	if !errors.As(err, &e) {
		return true
	}
	return e.Code() == psrpc.DeadlineExceeded
}

func retry(opt RetryOptions, done <-chan struct{}, fn func(timeout time.Duration) error) error {
	attempt := 1
	timeout := opt.Timeout
	if opt.IsRecoverable == nil {
		opt.IsRecoverable = isTimeout
	}

	for {
		err := fn(timeout)
		if err == nil || !opt.IsRecoverable(err) || attempt == opt.MaxAttempts {
			return err
		}
		select {
		case <-done:
			return psrpc.ErrRequestCanceled
		default:
		}

		attempt++
		timeout += opt.Backoff
	}
}

func WithStreamRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientStreamInterceptors(NewStreamRetryInterceptor(opt))
}

func NewStreamRetryInterceptor(opt RetryOptions) psrpc.StreamInterceptor {
	return func(info psrpc.RPCInfo, next psrpc.StreamHandler) psrpc.StreamHandler {
		return &streamRetryInterceptor{
			StreamHandler: next,
			opt:           opt,
		}
	}
}

type streamRetryInterceptor struct {
	psrpc.StreamHandler
	opt RetryOptions
}

func (s *streamRetryInterceptor) Send(msg proto.Message, opts ...psrpc.StreamOption) (err error) {
	return retry(s.opt, nil, func(timeout time.Duration) error {
		nextOpts := opts
		if timeout > 0 {
			nextOpts := make([]psrpc.StreamOption, len(opts)+1)
			copy(nextOpts, opts)
			nextOpts[len(opts)] = psrpc.WithTimeout(timeout)
		}

		return s.StreamHandler.Send(msg, nextOpts...)
	})
}
