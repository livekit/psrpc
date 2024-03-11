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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/livekit/psrpc/internal"
)

func serialize(msg proto.Message, channel string) ([]byte, error) {
	value, err := proto.Marshal(msg)
	if err != nil {
		return nil, err
	}

	return proto.Marshal(&internal.Msg{
		TypeUrl: "type.googleapis.com/" + string(msg.ProtoReflect().Descriptor().FullName()),
		Value:   value,
		Channel: channel,
	})
}

func deserializeChannel(b []byte) (string, error) {
	c := &internal.Channel{}
	opt := proto.UnmarshalOptions{
		DiscardUnknown: true,
	}
	err := opt.Unmarshal(b, c)
	if err != nil {
		return "", err
	}

	return c.Channel, nil
}

func deserialize(b []byte) (proto.Message, error) {
	a := &anypb.Any{}
	opt := proto.UnmarshalOptions{
		DiscardUnknown: true,
	}
	err := opt.Unmarshal(b, a)
	if err != nil {
		return nil, err
	}

	return a.UnmarshalNew()
}

func SerializePayload(m proto.Message) ([]byte, error) {
	return proto.Marshal(m)
}

func DeserializePayload[T proto.Message](buf []byte) (T, error) {
	var v T
	v = v.ProtoReflect().New().Interface().(T)
	return v, proto.Unmarshal(buf, v)
}
