package channels

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelFormatters(t *testing.T) {
	require.Equal(t, "foo|bar|a|b|c|REQ", RPCChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|REQ", RPCChannel("foo", "bar", nil))
	require.Equal(t, "foo|a|b|c", HandlerKey("foo", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|RES", ResponseChannel("foo", "bar"))
	require.Equal(t, "foo|bar|CLAIM", ClaimRequestChannel("foo", "bar"))
	require.Equal(t, "foo|bar|a|b|c|RCLAIM", ClaimResponseChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|RCLAIM", ClaimResponseChannel("foo", "bar", nil))
	require.Equal(t, "foo|bar|STR", StreamChannel("foo", "bar"))
	require.Equal(t, "foo|bar|a|b|c|STR", StreamServerChannel("foo", "bar", []string{"a", "b", "c"}))
	require.Equal(t, "foo|bar|STR", StreamServerChannel("foo", "bar", nil))

	require.Equal(t, "U+0001f680_u+00c9|U+0001f6f0_bar|u+8f6fu+4ef6|END", formatChannel("🚀_É", "🛰_bar", []string{"软件"}, "END"))
}
