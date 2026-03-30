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

package psrpc

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal/errors"
)

var (
	ErrRequestCanceled = errors.ErrRequestCanceled
	ErrRequestTimedOut = errors.ErrRequestTimedOut
	ErrNoResponse      = errors.ErrNoResponse
	ErrUnroutable      = errors.ErrUnroutable
	ErrUnimplemented   = errors.ErrUnimplemented
	ErrStreamEOF       = errors.ErrStreamEOF
	ErrClientClosed    = errors.ErrClientClosed
	ErrServerClosed    = errors.ErrServerClosed
	ErrStreamClosed    = errors.ErrStreamClosed
	ErrSlowConsumer    = errors.ErrSlowConsumer
)

type Error = errors.Error
type ErrorCode = errors.ErrorCode

const (
	OK                  = errors.OK
	Canceled            = errors.Canceled
	MalformedRequest    = errors.MalformedRequest
	MalformedResponse   = errors.MalformedResponse
	DeadlineExceeded    = errors.DeadlineExceeded
	Unavailable         = errors.Unavailable
	Unknown             = errors.Unknown
	InvalidArgument     = errors.InvalidArgument
	NotFound            = errors.NotFound
	NotAcceptable       = errors.NotAcceptable
	AlreadyExists       = errors.AlreadyExists
	PermissionDenied    = errors.PermissionDenied
	ResourceExhausted   = errors.ResourceExhausted
	FailedPrecondition  = errors.FailedPrecondition
	Aborted             = errors.Aborted
	OutOfRange          = errors.OutOfRange
	Unimplemented       = errors.Unimplemented
	Internal            = errors.Internal
	DataLoss            = errors.DataLoss
	Unauthenticated     = errors.Unauthenticated
	UnprocessableEntity = errors.UnprocessableEntity
	UpstreamServerError = errors.UpstreamServerError
	UpstreamClientError = errors.UpstreamClientError
)

func GetErrorCode(err error) (ErrorCode, bool) {
	return errors.GetErrorCode(err)
}

func ErrorCodeFromGRPC(code codes.Code) ErrorCode {
	return errors.ErrorCodeFromGRPC(code)
}

func NewError(code ErrorCode, err error, details ...proto.Message) Error {
	return errors.NewError(code, err, details...)
}

func NewErrorf(code ErrorCode, msg string, args ...interface{}) Error {
	return errors.NewErrorf(code, msg, args...)
}

func NewErrorFromResponse(code, err string, details ...*anypb.Any) Error {
	return errors.NewErrorFromResponse(code, err, details...)
}
