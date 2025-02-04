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
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/twitchtv/twirp"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var (
	ErrRequestCanceled = NewErrorf(Canceled, "request canceled")
	ErrRequestTimedOut = NewErrorf(DeadlineExceeded, "request timed out")
	ErrNoResponse      = NewErrorf(Unavailable, "no response from servers")
	ErrStreamEOF       = NewError(Unavailable, io.EOF)
	ErrClientClosed    = NewErrorf(Canceled, "client is closed")
	ErrServerClosed    = NewErrorf(Canceled, "server is closed")
	ErrStreamClosed    = NewErrorf(Canceled, "stream closed")
	ErrSlowConsumer    = NewErrorf(Unavailable, "stream message discarded by slow consumer")
)

type Error interface {
	error
	Code() ErrorCode
	Details() []any
	DetailsProto() []*anypb.Any

	// convenience methods
	ToHttp() int
	GRPCStatus() *status.Status
}

type ErrorCode string

func (e ErrorCode) Error() string {
	return string(e)
}

func (e ErrorCode) ToHTTP() int {
	switch e {
	case OK:
		return http.StatusOK
	case Unknown, MalformedResponse, Internal, DataLoss:
		return http.StatusInternalServerError
	case InvalidArgument, MalformedRequest:
		return http.StatusBadRequest
	case NotFound:
		return http.StatusNotFound
	case NotAcceptable:
		return http.StatusNotAcceptable
	case AlreadyExists, Aborted:
		return http.StatusConflict
	case PermissionDenied:
		return http.StatusForbidden
	case ResourceExhausted:
		return http.StatusTooManyRequests
	case FailedPrecondition:
		return http.StatusPreconditionFailed
	case OutOfRange:
		return http.StatusRequestedRangeNotSatisfiable
	case Unimplemented:
		return http.StatusNotImplemented
	case Canceled, DeadlineExceeded, Unavailable:
		return http.StatusServiceUnavailable
	case Unauthenticated:
		return http.StatusUnauthorized
	default:
		return http.StatusInternalServerError
	}
}

func ErrorCodeFromGRPC(code codes.Code) ErrorCode {
	switch code {
	case codes.OK:
		return OK
	case codes.Canceled:
		return Canceled
	case codes.Unknown:
		return Unknown
	case codes.InvalidArgument:
		return InvalidArgument
	case codes.DeadlineExceeded:
		return DeadlineExceeded
	case codes.NotFound:
		return NotFound
	case codes.AlreadyExists:
		return AlreadyExists
	case codes.PermissionDenied:
		return PermissionDenied
	case codes.ResourceExhausted:
		return ResourceExhausted
	case codes.FailedPrecondition:
		return FailedPrecondition
	case codes.Aborted:
		return Aborted
	case codes.OutOfRange:
		return OutOfRange
	case codes.Unimplemented:
		return Unimplemented
	case codes.Internal:
		return Internal
	case codes.Unavailable:
		return Unavailable
	case codes.DataLoss:
		return DataLoss
	case codes.Unauthenticated:
		return Unauthenticated
	default:
		return Unknown
	}
}

func (e ErrorCode) ToGRPC() codes.Code {
	switch e {
	case OK:
		return codes.OK
	case Canceled:
		return codes.Canceled
	case Unknown:
		return codes.Unknown
	case InvalidArgument, MalformedRequest:
		return codes.InvalidArgument
	case DeadlineExceeded:
		return codes.DeadlineExceeded
	case NotFound:
		return codes.NotFound
	case AlreadyExists:
		return codes.AlreadyExists
	case PermissionDenied:
		return codes.PermissionDenied
	case ResourceExhausted:
		return codes.ResourceExhausted
	case FailedPrecondition:
		return codes.FailedPrecondition
	case Aborted:
		return codes.Aborted
	case OutOfRange:
		return codes.OutOfRange
	case Unimplemented:
		return codes.Unimplemented
	case MalformedResponse, Internal:
		return codes.Internal
	case Unavailable:
		return codes.Unavailable
	case DataLoss:
		return codes.DataLoss
	case Unauthenticated:
		return codes.Unauthenticated
	default:
		return codes.Unknown
	}
}

func (e ErrorCode) ToTwirp() twirp.ErrorCode {
	switch e {
	case OK:
		return twirp.NoError
	case Canceled:
		return twirp.Canceled
	case Unknown:
		return twirp.Unknown
	case InvalidArgument:
		return twirp.InvalidArgument
	case MalformedRequest, MalformedResponse:
		return twirp.Malformed
	case DeadlineExceeded:
		return twirp.DeadlineExceeded
	case NotFound:
		return twirp.NotFound
	case AlreadyExists:
		return twirp.AlreadyExists
	case PermissionDenied:
		return twirp.PermissionDenied
	case ResourceExhausted:
		return twirp.ResourceExhausted
	case FailedPrecondition:
		return twirp.FailedPrecondition
	case Aborted:
		return twirp.Aborted
	case OutOfRange:
		return twirp.OutOfRange
	case Unimplemented:
		return twirp.Unimplemented
	case Internal:
		return twirp.Internal
	case Unavailable:
		return twirp.Unavailable
	case DataLoss:
		return twirp.DataLoss
	case Unauthenticated:
		return twirp.Unauthenticated
	default:
		return twirp.Unknown
	}
}

func NewError(code ErrorCode, err error, details ...proto.Message) Error {
	if err == nil {
		panic("error is nil")
	}
	var protoDetails []*anypb.Any
	for _, e := range details {
		if p, err := anypb.New(e); err == nil {
			protoDetails = append(protoDetails, p)
		}
	}
	return &psrpcError{
		error:   err,
		code:    code,
		details: protoDetails,
	}
}

func NewErrorf(code ErrorCode, msg string, args ...interface{}) Error {
	return &psrpcError{
		error: fmt.Errorf(msg, args...),
		code:  code,
	}
}

func NewErrorFromResponse(code, err string, details ...*anypb.Any) Error {
	if code == "" {
		code = string(Unknown)
	}

	return &psrpcError{
		error:   errors.New(err),
		code:    ErrorCode(code),
		details: details,
	}
}

const (
	OK ErrorCode = ""

	// Request Canceled by client
	Canceled ErrorCode = "canceled"
	// Could not unmarshal request
	MalformedRequest ErrorCode = "malformed_request"
	// Could not unmarshal result
	MalformedResponse ErrorCode = "malformed_result"
	// Request timed out
	DeadlineExceeded ErrorCode = "deadline_exceeded"
	// Service unavailable due to load and/or affinity constraints
	Unavailable ErrorCode = "unavailable"
	// Unknown (server returned non-psrpc error)
	Unknown ErrorCode = "unknown"

	// Invalid argument in request
	InvalidArgument ErrorCode = "invalid_argument"
	// Entity not found
	NotFound ErrorCode = "not_found"
	// Cannot produce and entity matching requested format
	NotAcceptable ErrorCode = "not_acceptable"
	// Duplicate creation attempted
	AlreadyExists ErrorCode = "already_exists"
	// Caller does not have required permissions
	PermissionDenied ErrorCode = "permission_denied"
	// Some resource has been exhausted, e.g. memory or quota
	ResourceExhausted ErrorCode = "resource_exhausted"
	// Inconsistent state to carry out request
	FailedPrecondition ErrorCode = "failed_precondition"
	// Request aborted
	Aborted ErrorCode = "aborted"
	// Operation was out of range
	OutOfRange ErrorCode = "out_of_range"
	// Operation is not implemented by the server
	Unimplemented ErrorCode = "unimplemented"
	// Operation failed due to an internal error
	Internal ErrorCode = "internal"
	// Irrecoverable loss or corruption of data
	DataLoss ErrorCode = "data_loss"
	// Similar to PermissionDenied, used when the caller is unidentified
	Unauthenticated ErrorCode = "unauthenticated"
)

type psrpcError struct {
	error
	code    ErrorCode
	details []*anypb.Any
}

func (e psrpcError) Code() ErrorCode {
	return e.code
}

func (e psrpcError) ToHttp() int {
	return e.code.ToHTTP()
}

func (e psrpcError) DetailsProto() []*anypb.Any {
	return e.details
}

func (e psrpcError) Details() []any {
	return e.GRPCStatus().Details()
}

func (e psrpcError) GRPCStatus() *status.Status {
	return status.FromProto(&spb.Status{
		Code:    int32(e.code.ToGRPC()),
		Message: e.Error(),
		Details: e.details,
	})
}

func (e psrpcError) toTwirp() twirp.Error {
	return twirp.NewError(e.code.ToTwirp(), e.Error())
}

func (e psrpcError) As(target any) bool {
	switch te := target.(type) {
	case *twirp.Error:
		*te = e.toTwirp()
		return true
	}

	return false
}

func (e psrpcError) Unwrap() []error {
	return []error{e.error, e.code}
}
