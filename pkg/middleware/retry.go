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
	"math/rand"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

type RetryOptions struct {
	MaxAttempts        int
	Timeout            time.Duration
	Backoff            time.Duration
	ExponentialBackoff bool // multiply backoff by [1, 1 + rand(1)) for each run
	IsRecoverable      func(err error) bool
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

		if opt.ExponentialBackoff {
			opt.Backoff = time.Duration(float32(opt.Backoff) * (1 + rand.Float32()))
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
