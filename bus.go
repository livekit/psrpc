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
	mqtt "github.com/eclipse/paho.mqtt.golang"
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

// ClosableMessageBus is implemented by buses that own their broker
// connections (AMQP, MQTT) and must be closed to release them.
type ClosableMessageBus interface {
	MessageBus
	Close() error
}

// NewMqttMessageBus connects to the given MQTT broker URLs, e.g.
// tcp://user:pass@localhost:1883 or ssl://host:8883. The broker must support
// shared subscriptions ($share), which Mosquitto 2.x and EMQX do even for
// 3.1.1 clients; unsupported brokers are detected and rejected at startup.
// The returned bus must be closed.
func NewMqttMessageBus(brokers []string, opts ...BusOption) (ClosableMessageBus, error) {
	b, err := bus.NewMqttMessageBus(func() *mqtt.ClientOptions {
		o := mqtt.NewClientOptions()
		for _, b := range brokers {
			o = o.AddBroker(b)
		}
		return o
	}, opts...)
	if err != nil {
		return nil, err
	}
	return b, nil
}
