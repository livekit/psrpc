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
	"strings"
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

	require.Equal(t, "foo|bar|RES", GetResponseChannel("foo", "bar").Legacy)
	require.Equal(t, "CLI.foo.bar.RES", GetResponseChannel("foo", "bar").Server)
	require.Equal(t, "foo|bar|CLAIM", GetClaimRequestChannel("foo", "bar").Legacy)
	require.Equal(t, "CLI.foo.bar.CLAIM", GetClaimRequestChannel("foo", "bar").Server)
	require.Equal(t, "foo|bar|STR", GetStreamChannel("foo", "bar").Legacy)
	require.Equal(t, "CLI.foo.bar.STR", GetStreamChannel("foo", "bar").Server)

	require.Equal(t, "foo|bar|REQ", i.GetRPCChannel().Legacy)
	require.Equal(t, "SRV.foo", i.GetRPCChannel().Server)
	require.Equal(t, "bar.REQ", i.GetRPCChannel().Local)
	require.Equal(t, "foo|bar|RCLAIM", i.GetClaimResponseChannel().Legacy)
	require.Equal(t, "SRV.foo", i.GetClaimResponseChannel().Server)
	require.Equal(t, "bar.RCLAIM", i.GetClaimResponseChannel().Local)
	require.Equal(t, "foo|bar|STR", i.GetStreamServerChannel().Legacy)
	require.Equal(t, "SRV.foo", i.GetStreamServerChannel().Server)
	require.Equal(t, "bar.STR", i.GetStreamServerChannel().Local)

	i.Topic = []string{"a", "b", "c"}

	require.Equal(t, "foo|bar|a|b|c|REQ", i.GetRPCChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c", i.GetRPCChannel().Server)
	require.Equal(t, "bar.REQ", i.GetRPCChannel().Local)
	require.Equal(t, "bar.a.b.c", i.GetHandlerKey())
	require.Equal(t, "foo|bar|a|b|c|RCLAIM", i.GetClaimResponseChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c", i.GetClaimResponseChannel().Server)
	require.Equal(t, "bar.RCLAIM", i.GetClaimResponseChannel().Local)
	require.Equal(t, "foo|bar|a|b|c|STR", i.GetStreamServerChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c", i.GetStreamServerChannel().Server)
	require.Equal(t, "bar.STR", i.GetStreamServerChannel().Local)

	i.Queue = true

	require.Equal(t, "foo|bar|a|b|c|REQ", i.GetRPCChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c.Q", i.GetRPCChannel().Server)
	require.Equal(t, "bar.REQ", i.GetRPCChannel().Local)
	require.Equal(t, "foo|bar|a|b|c|RCLAIM", i.GetClaimResponseChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c", i.GetClaimResponseChannel().Server)
	require.Equal(t, "bar.RCLAIM", i.GetClaimResponseChannel().Local)
	require.Equal(t, "foo|bar|a|b|c|STR", i.GetStreamServerChannel().Legacy)
	require.Equal(t, "SRV.foo.a.b.c", i.GetStreamServerChannel().Server)
	require.Equal(t, "bar.STR", i.GetStreamServerChannel().Local)

	require.Equal(t, "U+0001f680_u+00c9.U+0001f6f0_bar.u+8f6fu+4ef6.END", formatChannel('.', "🚀_É", "🛰_bar", []string{"软件"}, "END"))
}

func TestServerChannelLengthBound(t *testing.T) {
	channel := func(queue bool, topic ...string) string {
		i := &RequestInfo{RPCInfo: psrpc.RPCInfo{Service: "foo", Method: "bar", Topic: topic}, Queue: queue}
		return i.GetRPCChannel().Server
	}

	t.Run("topic at the bound is unchanged", func(t *testing.T) {
		topic := strings.Repeat("a", maxServerChannelLength-1) // plus the '.' separator
		require.Equal(t, "SRV.foo."+topic, channel(false, topic))
	})

	t.Run("topic over the bound is hashed", func(t *testing.T) {
		long := strings.Repeat("A", 8000)
		c := channel(false, "p_project", long)
		require.Regexp(t, `^SRV\.foo\.h-[0-9a-f]{32}$`, c)
		require.Equal(t, c, channel(false, "p_project", long), "hash must be stable")
		require.NotEqual(t, c, channel(false, "p_other", long))
		require.Regexp(t, `^SRV\.foo\.h-[0-9a-f]{32}\.Q$`, channel(true, "p_project", long))
	})

	t.Run("sanitized expansion counts toward the bound", func(t *testing.T) {
		// 300 runes outside the BMP expand to 10 bytes each
		require.Regexp(t, `^SRV\.foo\.h-[0-9a-f]{32}$`, channel(false, strings.Repeat("🚀", 300)))
	})

	t.Run("hashed topic does not add an empty token", func(t *testing.T) {
		// with an empty service, the prefix "SRV." already ends with '.'
		c := formatServerChannel("", []string{strings.Repeat("A", 8000)}, true)
		require.Regexp(t, `^SRV\.h-[0-9a-f]{32}\.Q$`, c)
		require.NotContains(t, c, "..")
	})

	t.Run("all server channels agree", func(t *testing.T) {
		i := &RequestInfo{RPCInfo: psrpc.RPCInfo{Service: "foo", Method: "bar", Topic: []string{strings.Repeat("A", 8000)}}}
		require.Equal(t, i.GetRPCChannel().Server, i.GetClaimResponseChannel().Server)
		require.Equal(t, i.GetRPCChannel().Server, i.GetStreamServerChannel().Server)
	})
}
