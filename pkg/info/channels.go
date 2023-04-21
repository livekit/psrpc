package info

import (
	"unicode"
)

const lowerHex = "0123456789abcdef"

var channelChar = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x0030, 0x0039, 1}, // 0-9
		{0x0041, 0x005a, 1}, // A-Z
		{0x005f, 0x005f, 1}, // _
		{0x0061, 0x007a, 1}, // a-z
	},
	LatinOffset: 4,
}

func GetClaimRequestChannel(service, clientID string) string {
	return formatChannel(service, clientID, "CLAIM")
}

func GetStreamChannel(service, nodeID string) string {
	return formatChannel(service, nodeID, "STR")
}

func GetResponseChannel(service, clientID string) string {
	return formatChannel(service, clientID, "RES")
}

func (i *RequestInfo) GetRPCChannel() string {
	return formatChannel(i.Service, i.Method, i.Topic, "REQ")
}

func (i *RequestInfo) GetHandlerKey() string {
	return formatChannel(i.Method, i.Topic)
}

func (i *RequestInfo) GetClaimResponseChannel() string {
	return formatChannel(i.Service, i.Method, i.Topic, "RCLAIM")
}

func (i *RequestInfo) GetStreamServerChannel() string {
	return formatChannel(i.Service, i.Method, i.Topic, "STR")
}

func formatChannel(parts ...any) string {
	buf := make([]byte, 0, 4*channelPartsLen(parts...)/3)
	return string(appendChannelParts(buf, parts...))
}

func channelPartsLen[T any](parts ...T) int {
	var n int
	for _, t := range parts {
		switch v := any(t).(type) {
		case string:
			n += len(v) + 1
		case []string:
			n += channelPartsLen(v...)
		}
	}
	return n
}

func appendChannelParts[T any](buf []byte, parts ...T) []byte {
	var prefix bool
	for _, t := range parts {
		if prefix {
			buf = append(buf, '|')
		}
		l := len(buf)
		switch v := any(t).(type) {
		case string:
			buf = appendSanitizedChannelPart(buf, v)
		case []string:
			buf = appendChannelParts(buf, v...)
		}
		prefix = len(buf) > l
	}
	return buf
}

func appendSanitizedChannelPart(buf []byte, s string) []byte {
	for _, r := range s {
		if unicode.Is(channelChar, r) {
			buf = append(buf, byte(r))
		} else if r < 0x10000 {
			buf = append(buf, `u+`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerHex[r>>uint(s)&0xF])
			}
		} else {
			buf = append(buf, `U+`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerHex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}
