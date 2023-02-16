package psrpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestSerialization(t *testing.T) {
	msg := &internal.Request{
		RequestId: "reid",
		ClientId:  "clid",
		SentAt:    time.Now().UnixNano(),
		Multi:     true,
	}

	b, err := serialize(msg)
	require.NoError(t, err)

	m, err := deserialize(b)
	require.NoError(t, err)

	require.Equal(t, m.(*internal.Request).RequestId, msg.RequestId)
	require.Equal(t, m.(*internal.Request).ClientId, msg.ClientId)
	require.Equal(t, m.(*internal.Request).SentAt, msg.SentAt)
	require.Equal(t, m.(*internal.Request).Multi, msg.Multi)
}

func TestChannelFormatters(t *testing.T) {
	require.Equal(t, "foo|bar|a|b|c|REQ", getRPCChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|REQ", getRPCChannel("foo", "bar", nil))
	require.Equal(t, "foo|a|b|c", getHandlerKey("foo", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|RES", getResponseChannel("foo", "bar"))
	require.Equal(t, "foo|bar|CLAIM", getClaimRequestChannel("foo", "bar"))
	require.Equal(t, "foo|bar|a|b|c|RCLAIM", getClaimResponseChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|RCLAIM", getClaimResponseChannel("foo", "bar", nil))
	require.Equal(t, "foo|bar|STR", getStreamChannel("foo", "bar"))
	require.Equal(t, "foo|bar|a|b|c|STR", getStreamServerChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|STR", getStreamServerChannel("foo", "bar", nil))

	require.Equal(t, "U+0001f680_u+00c9|U+0001f6f0u+fe0f_bar|u+8f6fu+4ef6|END", formatChannel("🚀_É", "🛰️_bar", []string{"软件"}, "END"))
}
