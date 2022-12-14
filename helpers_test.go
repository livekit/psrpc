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
