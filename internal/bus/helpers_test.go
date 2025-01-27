package bus

import "google.golang.org/protobuf/proto"

// These helpers are only exposed during tests.

func RawRead(r Reader) ([]byte, bool) {
	return r.read()
}

func Deserialize(b []byte) (proto.Message, error) {
	return deserialize(b)
}
