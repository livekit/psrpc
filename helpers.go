package psrpc

import (
	"unicode"

	"github.com/lithammer/shortuuid/v3"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func newRequestID() string {
	return "REQ_" + shortuuid.New()[:12]
}

func newStreamID() string {
	return "STR_" + shortuuid.New()[:12]
}

const lowerhex = "0123456789abcdef"

var channelChar = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x0030, 0x0039, 1}, // 0-9
		{0x0041, 0x005a, 1}, // A-Z
		{0x005f, 0x005f, 1}, // _
		{0x0061, 0x007a, 1}, // a-z
	},
	LatinOffset: 4,
}

func appendSanitizedChannelPart(buf []byte, s string) []byte {
	for _, r := range s {
		if unicode.Is(channelChar, r) {
			buf = append(buf, byte(r))
		} else if r < 0x10000 {
			buf = append(buf, `u+`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		} else {
			buf = append(buf, `U+`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}

func formatChannel(prefix0, prefix1 string, topics []string, suffix string) string {
	l := len(prefix0) + len(prefix1) + len(suffix)
	for _, t := range topics {
		l += len(t)
	}
	buf := make([]byte, 0, 4*l/3)
	buf = appendSanitizedChannelPart(buf, prefix0)
	if prefix1 != "" {
		buf = append(buf, `|`...)
		buf = appendSanitizedChannelPart(buf, prefix1)
	}
	for _, s := range topics {
		buf = append(buf, `|`...)
		buf = appendSanitizedChannelPart(buf, s)
	}
	if suffix != "" {
		buf = append(buf, `|`...)
		buf = appendSanitizedChannelPart(buf, suffix)
	}
	return string(buf)
}

func getRPCChannel(serviceName, rpc string, topic []string) string {
	return formatChannel(serviceName, rpc, topic, "REQ")
}

func getHandlerKey(rpc string, topic []string) string {
	return formatChannel(rpc, "", topic, "")
}

func getResponseChannel(serviceName, clientID string) string {
	return formatChannel(serviceName, clientID, nil, "RES")
}

func getClaimRequestChannel(serviceName, clientID string) string {
	return formatChannel(serviceName, clientID, nil, "CLAIM")
}

func getClaimResponseChannel(serviceName, rpc string, topic []string) string {
	return formatChannel(serviceName, rpc, topic, "RCLAIM")
}

func getStreamChannel(serviceName, nodeID string) string {
	return formatChannel(serviceName, nodeID, nil, "STR")
}

func getStreamServerChannel(serviceName, rpc string, topic []string) string {
	return formatChannel(serviceName, rpc, topic, "STR")
}

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
