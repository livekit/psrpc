// Copyright 2026 LiveKit, Inc.
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
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
)

func TestServerRecoveryDoesNotExposePanicDetails(t *testing.T) {
	interceptor := WithServerRecovery()
	_, err := interceptor(
		context.Background(), nil, psrpc.RPCInfo{},
		func(context.Context, proto.Message) (proto.Message, error) {
			panic("sensitive panic value")
		},
	)

	require.Error(t, err)
	code, ok := psrpc.GetErrorCode(err)
	require.True(t, ok)
	require.Equal(t, psrpc.Internal, code)
	require.EqualError(t, err, "server handler panic")
	require.NotContains(t, err.Error(), "sensitive panic value")
	require.NotContains(t, err.Error(), "recovery_test.go")
}
