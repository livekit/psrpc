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
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/localbus"
)

type Channel = bus.Channel
type MessageBus = bus.MessageBus
type Reader = bus.Reader

type Transport = bus.Transport

type BusOption = bus.BusOption
type CompressionOpts = bus.CompressionOpts

const DefaultCompressionThreshold = bus.DefaultCompressionThreshold

// WithBusCompression gzips published payloads above a size threshold.
func WithBusCompression(c CompressionOpts) BusOption {
	return bus.WithBusCompression(c)
}

// Aliased here, unlike the broker-backed buses, because localbus adds nothing
// to this package's dependency closure.
func NewLocalMessageBus(opts ...BusOption) MessageBus {
	return localbus.New(opts...)
}

// ClosableMessageBus is implemented by buses that own their broker
// connections (AMQP, MQTT) and must be closed to release them.
type ClosableMessageBus interface {
	MessageBus
	Close() error
}

// NewAmqpMessageBus connects to an AMQP 0-9-1 broker such as RabbitMQ, e.g.
// amqp://user:pass@localhost:5672/ or amqps://host:5671/vhost. The returned
// bus must be closed.
func NewAmqpMessageBus(url string, opts ...BusOption) (ClosableMessageBus, error) {
	b, err := bus.NewAmqpMessageBus(url, opts...)
	if err != nil {
		return nil, err
	}
	return b, nil
}
