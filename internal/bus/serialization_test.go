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
	"crypto/rand"
	"strings"
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

	b, err := serialize(msg, "channel", nil)
	require.NoError(t, err)

	m, err := deserialize(b, 0)
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

func compressibleRequest() *internal.Request {
	return &internal.Request{
		RequestId:  "reid",
		ClientId:   "clid",
		SentAt:     time.Now().UnixNano(),
		Multi:      true,
		RawRequest: []byte(strings.Repeat("psrpc ", 500)),
	}
}

func TestSerializeCompressed(t *testing.T) {
	msg := compressibleRequest()

	plain, err := serialize(msg, "channel", nil)
	require.NoError(t, err)

	b, err := serialize(msg, "channel", testCompressor(6, 1))
	require.NoError(t, err)
	require.Less(t, len(b), len(plain))

	var envelope internal.Msg
	require.NoError(t, proto.Unmarshal(b, &envelope))
	require.Equal(t, internal.Compression_COMPRESSION_GZIP, envelope.Compression)

	channel, err := deserializeChannel(b)
	require.NoError(t, err)
	require.Equal(t, "channel", channel)

	m, err := deserialize(b, 0)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, m))
}

func TestSerializeBelowThresholdStaysPlain(t *testing.T) {
	msg := compressibleRequest()

	b, err := serialize(msg, "channel", testCompressor(6, 1<<20))
	require.NoError(t, err)

	var envelope internal.Msg
	require.NoError(t, proto.Unmarshal(b, &envelope))
	require.Equal(t, internal.Compression_COMPRESSION_NONE, envelope.Compression)

	m, err := deserialize(b, 0)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, m))
}

func TestSerializeIncompressibleStaysPlain(t *testing.T) {
	raw := make([]byte, 4096)
	_, err := rand.Read(raw)
	require.NoError(t, err)

	msg := &internal.Request{RequestId: "reid", RawRequest: raw}
	b, err := serialize(msg, "channel", testCompressor(9, 1))
	require.NoError(t, err)

	var envelope internal.Msg
	require.NoError(t, proto.Unmarshal(b, &envelope))
	require.Equal(t, internal.Compression_COMPRESSION_NONE, envelope.Compression)

	m, err := deserialize(b, 0)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, m))
}

func TestDeserializeUncompressedPeer(t *testing.T) {
	msg := compressibleRequest()
	value, err := proto.Marshal(msg)
	require.NoError(t, err)

	b, err := proto.Marshal(&internal.Msg{
		TypeUrl: "type.googleapis.com/" + string(msg.ProtoReflect().Descriptor().FullName()),
		Value:   value,
		Channel: "channel",
	})
	require.NoError(t, err)

	m, err := deserialize(b, 0)
	require.NoError(t, err)
	require.True(t, proto.Equal(msg, m))
}

// proto3 enums are open, so an unknown codec survives unmarshal as a live value
// rather than being dropped as an unknown field.
func TestDeserializeUnknownCompression(t *testing.T) {
	b, err := proto.Marshal(&internal.Msg{
		TypeUrl:     "type.googleapis.com/internal.Request",
		Value:       []byte("whatever"),
		Compression: internal.Compression(99),
	})
	require.NoError(t, err)

	_, err = deserialize(b, 0)
	require.ErrorContains(t, err, "unrecognized message compression")
}

func TestDeserializeCompressedExceedsMaxSize(t *testing.T) {
	b, err := serialize(compressibleRequest(), "channel", testCompressor(6, 1))
	require.NoError(t, err)

	_, err = deserialize(b, 64)
	require.ErrorContains(t, err, "exceeds")
}
