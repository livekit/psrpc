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

package psrpc

import (
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/internal/bus"
)

type Channel = bus.Channel
type MessageBus = bus.MessageBus
type Reader = bus.Reader

type BusOption = bus.BusOption
type CompressionOpts = bus.CompressionOpts

const DefaultCompressionThreshold = bus.DefaultCompressionThreshold

// WithBusCompression gzips published payloads above a size threshold.
func WithBusCompression(c CompressionOpts) BusOption {
	return bus.WithBusCompression(c)
}

func NewLocalMessageBus(opts ...BusOption) MessageBus {
	return bus.NewLocalMessageBus(opts...)
}

func NewNatsMessageBus(nc *nats.Conn, opts ...BusOption) MessageBus {
	return bus.NewNatsMessageBus(nc, opts...)
}

func NewRedisMessageBus(rc redis.UniversalClient, opts ...BusOption) MessageBus {
	return bus.NewRedisMessageBus(rc, opts...)
}
