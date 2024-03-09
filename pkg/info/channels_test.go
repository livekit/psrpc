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

package info

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
)

func TestChannelFormatters(t *testing.T) {
	i := &RequestInfo{
		RPCInfo: psrpc.RPCInfo{
			Service: "foo",
			Method:  "bar",
			Topic:   nil,
		},
	}

	require.Equal(t, "foo.bar.RES", GetResponseChannel("foo", "bar").Primary)
	require.Equal(t, "foo.bar.CLAIM", GetClaimRequestChannel("foo", "bar").Primary)
	require.Equal(t, "foo.bar.STR", GetStreamChannel("foo", "bar").Primary)

	require.Equal(t, "foo.bar.REQ", i.GetRPCChannel().Primary)
	require.Equal(t, "foo.*.*", i.GetRPCChannel().Wildcard)
	require.Equal(t, "foo.bar.RCLAIM", i.GetClaimResponseChannel().Primary)
	require.Equal(t, "foo.*.*", i.GetClaimResponseChannel().Wildcard)
	require.Equal(t, "foo.bar.STR", i.GetStreamServerChannel().Primary)
	require.Equal(t, "foo.*.*", i.GetStreamServerChannel().Wildcard)

	i.Topic = []string{"a", "b", "c"}

	require.Equal(t, "foo.bar.a.b.c.REQ", i.GetRPCChannel().Primary)
	require.Equal(t, "foo.*.a.b.c.*", i.GetRPCChannel().Wildcard)
	require.Equal(t, "bar.a.b.c", i.GetHandlerKey())
	require.Equal(t, "foo.bar.a.b.c.RCLAIM", i.GetClaimResponseChannel().Primary)
	require.Equal(t, "foo.*.a.b.c.*", i.GetClaimResponseChannel().Wildcard)
	require.Equal(t, "foo.bar.a.b.c.STR", i.GetStreamServerChannel().Primary)
	require.Equal(t, "foo.*.a.b.c.*", i.GetStreamServerChannel().Wildcard)

	require.Equal(t, "U+0001f680_u+00c9.U+0001f6f0_bar.u+8f6fu+4ef6.END", formatChannel('.', "🚀_É", "🛰_bar", []string{"软件"}, "END"))
}
