package bus

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func serialize(msg proto.Message) ([]byte, error) {
	a, err := anypb.New(msg)
	if err != nil {
		return nil, err
	}

	b, err := proto.Marshal(a)
	if err != nil {
		return nil, err
	}

	return b, nil
}

func deserialize(b []byte) (proto.Message, error) {
	a := &anypb.Any{}
	err := proto.Unmarshal(b, a)
	if err != nil {
		return nil, err
	}

	return a.UnmarshalNew()
}

func SerializePayload(m proto.Message) ([]byte, error) {
	return proto.Marshal(m)
}

func DeserializePayload[T proto.Message](buf []byte) (T, error) {
	var p T
	v := p.ProtoReflect().New().Interface().(T)
	return v, proto.Unmarshal(buf, v)
}
