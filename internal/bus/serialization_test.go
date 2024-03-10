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

package bus

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

func TestSerialization(t *testing.T) {
	msg := &internal.Request{
		RequestId: "reid",
		ClientId:  "clid",
		SentAt:    time.Now().UnixNano(),
		Multi:     true,
	}

	b, err := serialize(msg, "channel")
	require.NoError(t, err)

	m, err := deserialize(b)
	require.NoError(t, err)

	channel, err := deserializeChannel(b)
	require.NoError(t, err)

	require.Equal(t, m.(*internal.Request).RequestId, msg.RequestId)
	require.Equal(t, m.(*internal.Request).ClientId, msg.ClientId)
	require.Equal(t, m.(*internal.Request).SentAt, msg.SentAt)
	require.Equal(t, m.(*internal.Request).Multi, msg.Multi)
	require.Equal(t, "channel", channel)
}

func TestRawSerialization(t *testing.T) {
	msg := &internal.Request{
		RequestId: "reid",
		ClientId:  "clid",
		SentAt:    time.Now().UnixNano(),
		Multi:     true,
	}

	b, err := SerializePayload(msg)
	require.NoError(t, err)

	msg0, err := DeserializePayload[*internal.Request](b)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, msg0), "expected deserialized payload to match source")

	msg1, err := DeserializePayload[*internal.Request](b)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, msg1), "expected deserialized payload to match source")
}
