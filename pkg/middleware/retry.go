// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package middleware

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

type RetryOptions struct {
	MaxAttempts        int
	Timeout            time.Duration
	Backoff            time.Duration
	IsRecoverable      func(err error) bool
	GetRetryParameters func(err error, attempt int) (retry bool, timeout time.Duration, waitTime time.Duration) // will override the MaxAttempts, Timeout and Backoff parameters
}

func WithRPCRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientRPCInterceptors(NewRPCRetryInterceptor(opt))
}

func NewRPCRetryInterceptor(opt RetryOptions) psrpc.ClientRPCInterceptor {
	return func(rpcInfo psrpc.RPCInfo, next psrpc.ClientRPCHandler) psrpc.ClientRPCHandler {
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
	return e.Code() == psrpc.DeadlineExceeded || e.Code() == psrpc.Unavailable
}

func getRetryWithBackoffParameters(o RetryOptions) func(err error, attempt int) (retry bool, timeout time.Duration, waitTime time.Duration) {
	timeout := o.Timeout

	return func(err error, attempt int) (bool, time.Duration, time.Duration) {
		if !o.IsRecoverable(err) || attempt == o.MaxAttempts {
			return false, 0, 0
		}

		timeout += o.Backoff

		return true, timeout, 0
	}
}

func retry(opt RetryOptions, done <-chan struct{}, fn func(timeout time.Duration) error) error {
	timeout := opt.Timeout
	attempt := 1
	if opt.IsRecoverable == nil {
		opt.IsRecoverable = isTimeout
	}

	if opt.GetRetryParameters == nil {
		opt.GetRetryParameters = getRetryWithBackoffParameters(opt)
	}

	for {
		err := fn(timeout)
		if err == nil {
			return nil
		}

		var retry bool
		var waitTime time.Duration
		retry, timeout, waitTime = opt.GetRetryParameters(err, attempt)
		if !retry {
			return err
		}

		attempt++

		select {
		case <-done:
			return psrpc.ErrRequestCanceled
		case <-time.After(waitTime):
		}
	}
}

func WithStreamRetries(opt RetryOptions) psrpc.ClientOption {
	return psrpc.WithClientStreamInterceptors(NewStreamRetryInterceptor(opt))
}

func NewStreamRetryInterceptor(opt RetryOptions) psrpc.StreamInterceptor {
	return func(rpcInfo psrpc.RPCInfo, next psrpc.StreamHandler) psrpc.StreamHandler {
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
			nextOpts = make([]psrpc.StreamOption, len(opts)+1)
			copy(nextOpts, opts)
			nextOpts[len(opts)] = psrpc.WithTimeout(timeout)
		}

		return s.StreamHandler.Send(msg, nextOpts...)
	})
}
