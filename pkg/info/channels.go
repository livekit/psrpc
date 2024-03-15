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

package info

import (
	"sync"
	"unicode"

	"github.com/livekit/psrpc/internal/bus"
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

func GetClaimRequestChannel(service, clientID string) bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', service, clientID, "CLAIM"),
		Server: formatClientChannel(service, clientID, "CLAIM"),
	}
}

func GetStreamChannel(service, nodeID string) bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', service, nodeID, "STR"),
		Server: formatClientChannel(service, nodeID, "STR"),
	}
}

func GetResponseChannel(service, clientID string) bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', service, clientID, "RES"),
		Server: formatClientChannel(service, clientID, "RES"),
	}
}

func (i *RequestInfo) GetRPCChannel() bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', i.Service, i.Method, i.Topic, "REQ"),
		Server: formatServerChannel(i.Service, i.Topic, i.Queue),
		Local:  formatLocalChannel(i.Method, "REQ"),
	}
}

func (i *RequestInfo) GetHandlerKey() string {
	return formatChannel('.', i.Method, i.Topic)
}

func (i *RequestInfo) GetClaimResponseChannel() bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', i.Service, i.Method, i.Topic, "RCLAIM"),
		Server: formatServerChannel(i.Service, i.Topic, false),
		Local:  formatLocalChannel(i.Method, "RCLAIM"),
	}
}

func (i *RequestInfo) GetStreamServerChannel() bus.Channel {
	return bus.Channel{
		Legacy: formatChannel('|', i.Service, i.Method, i.Topic, "STR"),
		Server: formatServerChannel(i.Service, i.Topic, false),
		Local:  formatLocalChannel(i.Method, "STR"),
	}
}

var scratch = &sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

func formatClientChannel(service, clientID, channel string) string {
	p := scratch.Get().(*[]byte)
	defer scratch.Put(p)
	b := append(*p, "CLI."...)
	b = append(b, service...)
	b = append(b, '.')
	b = append(b, clientID...)
	b = append(b, '.')
	b = append(b, channel...)
	return string(b)
}

func formatLocalChannel(method, channel string) string {
	p := scratch.Get().(*[]byte)
	defer scratch.Put(p)
	b := append(*p, method...)
	b = append(b, '.')
	b = append(b, channel...)
	return string(b)
}

func formatServerChannel(service string, topic []string, queue bool) string {
	p := scratch.Get().(*[]byte)
	defer scratch.Put(p)
	b := append(*p, "SRV."...)
	b = append(b, service...)
	for _, t := range topic {
		if len(t) != 0 {
			b = append(b, '.')
			b = appendSanitizedChannelPart(b, t)
		}
	}
	if queue {
		b = append(b, ".Q"...)
	}
	return string(b)
}

func formatChannel(delim byte, parts ...any) string {
	buf := make([]byte, 0, 4*channelPartsLen(parts...)/3)
	return string(appendChannelParts(buf, delim, parts...))
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

func appendChannelParts[T any](buf []byte, delim byte, parts ...T) []byte {
	var prefix bool
	for _, t := range parts {
		if prefix {
			buf = append(buf, delim)
		}
		l := len(buf)
		switch v := any(t).(type) {
		case string:
			buf = appendSanitizedChannelPart(buf, v)
		case []string:
			buf = appendChannelParts(buf, delim, v...)
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
