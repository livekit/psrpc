package middleware

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/livekit/psrpc"
)

// driverErr stands in for the class of error this interceptor exists to contain: text
// produced by a dependency, never written with a caller in mind.
var driverErr = errors.New(`ERROR: column "from_host" does not exist (SQLSTATE 42703)`)

// internalMarkers are strings that must never appear in a caller-visible message
var internalMarkers = []string{"SQLSTATE", "42703", "from_host", "does not exist"}

func requireNoInternalDetail(t *testing.T, msg string) {
	t.Helper()
	for _, m := range internalMarkers {
		require.NotContains(t, msg, m, "caller-visible message leaks internal detail")
	}
}

type testLogger struct {
	warnings []error
}

func (l *testLogger) Warnw(_ string, err error, _ ...any) {
	l.warnings = append(l.warnings, err)
}

func TestNeedsMasking(t *testing.T) {
	var cases = []struct {
		Name string
		Err  error
		Exp  bool
	}{
		{Name: "nil", Err: nil, Exp: false},
		{Name: "plain", Err: errors.New("boom"), Exp: true},
		{Name: "driver", Err: driverErr, Exp: true},
		{Name: "context canceled", Err: context.Canceled, Exp: true},

		{Name: "psrpc unknown", Err: psrpc.NewErrorf(psrpc.Unknown, "pq: relation does not exist"), Exp: true},
		{Name: "psrpc internal", Err: psrpc.NewErrorf(psrpc.Internal, "failed to list phone numbers"), Exp: false},
		{Name: "psrpc not found", Err: psrpc.NewErrorf(psrpc.NotFound, "trunk not found"), Exp: false},
		{Name: "psrpc invalid argument", Err: psrpc.NewErrorf(psrpc.InvalidArgument, "missing projectID"), Exp: false},

		// These two codes have no case in ErrorCode.ToGRPC, so they map to codes.Unknown.
		// Reading the code back out of GRPCStatus would mask them.
		{Name: "psrpc not acceptable", Err: psrpc.NewErrorf(psrpc.NotAcceptable, "unsupported codec"), Exp: false},
		{Name: "psrpc unprocessable entity", Err: psrpc.NewErrorf(psrpc.UnprocessableEntity, "unroutable dispatch rule"), Exp: false},

		{Name: "grpc unknown", Err: status.Error(codes.Unknown, "pq: relation does not exist"), Exp: true},
		{Name: "grpc internal", Err: status.Error(codes.Internal, "failed to search vendor inventory"), Exp: false},
		{Name: "grpc canceled", Err: status.Error(codes.Canceled, "canceled"), Exp: false},
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			require.Equal(t, c.Exp, needsMasking(c.Err))
			if c.Err != nil {
				require.Equal(t, c.Exp, needsMasking(fmt.Errorf("wrapped: %w", c.Err)),
					"wrapping must not change the verdict")
			}
		})
	}
}

// TestNeedsMaskingRelayed covers an unclassified error that has already crossed a psrpc
// boundary. The server serialized it as raw text with code Unknown and the client rebuilt
// it with NewErrorFromResponse, so it arrives coded. It must still be masked, otherwise a
// server relaying a response from another server would pass the leak along.
func TestNeedsMaskingRelayed(t *testing.T) {
	require.True(t, needsMasking(psrpc.NewErrorFromResponse(string(psrpc.Unknown), driverErr.Error())))
	require.True(t, needsMasking(psrpc.NewErrorFromResponse("", driverErr.Error())))
}

func TestWithServerErrorMasking(t *testing.T) {
	callReturning := func(l Logger, handlerRes proto.Message, handlerErr error) (proto.Message, error) {
		return WithServerErrorMasking(l)(
			context.Background(),
			nil,
			psrpc.RPCInfo{Service: "IOInfo", Method: "GetEgress"},
			func(context.Context, proto.Message) (proto.Message, error) { return handlerRes, handlerErr },
		)
	}

	t.Run("an unclassified error is masked and reported", func(t *testing.T) {
		l := &testLogger{}

		_, err := callReturning(l, nil, fmt.Errorf("failed to get egress: %w", driverErr))
		require.Error(t, err)
		require.Equal(t, InternalErrorMessage, err.Error())
		requireNoInternalDetail(t, err.Error())

		code, ok := psrpc.GetErrorCode(err)
		require.True(t, ok)
		require.Equal(t, psrpc.Internal, code)

		// The detail must survive somewhere, or masking would simply lose it
		require.Len(t, l.warnings, 1)
		require.ErrorIs(t, l.warnings[0], driverErr)
	})

	t.Run("the cause is not reachable through the masked error", func(t *testing.T) {
		_, err := callReturning(&testLogger{}, nil, psrpc.NewError(psrpc.Unknown, driverErr))

		require.NotErrorIs(t, err, driverErr, "unwrapping must not reach the cause")
		requireNoInternalDetail(t, err.Error())
	})

	t.Run("a deliberate code and message survive", func(t *testing.T) {
		l := &testLogger{}

		_, err := callReturning(l, nil, psrpc.NewErrorf(psrpc.NotFound, "egress not found"))
		require.Equal(t, "egress not found", err.Error())

		code, ok := psrpc.GetErrorCode(err)
		require.True(t, ok)
		require.Equal(t, psrpc.NotFound, code)

		require.Empty(t, l.warnings, "a coded error is not something to warn about")
	})

	// NotAcceptable has no ErrorCode.ToGRPC case, so a predicate reading the code out of
	// GRPCStatus would see codes.Unknown here and mask a deliberately coded error.
	t.Run("a code with no gRPC equivalent survives", func(t *testing.T) {
		_, err := callReturning(&testLogger{}, nil, psrpc.NewErrorf(psrpc.NotAcceptable, "unsupported codec"))
		require.Equal(t, "unsupported codec", err.Error())

		code, ok := psrpc.GetErrorCode(err)
		require.True(t, ok)
		require.Equal(t, psrpc.NotAcceptable, code)
	})

	t.Run("success passes through", func(t *testing.T) {
		handlerRes := &emptypb.Empty{}

		res, err := callReturning(&testLogger{}, handlerRes, nil)
		require.NoError(t, err)
		require.Same(t, handlerRes, res)
	})

	// Only the error is replaced. psrpc ignores the response when the error is non-nil,
	// and res can only ever be what the handler returned, so there is nothing to gain from
	// dropping it.
	t.Run("a response returned alongside an error passes through", func(t *testing.T) {
		handlerRes := &emptypb.Empty{}

		res, err := callReturning(&testLogger{}, handlerRes, driverErr)
		require.Error(t, err)
		require.Same(t, handlerRes, res, "the handler's response must be returned unchanged")
	})
}
