// Copyright 2025 LiveKit, Inc.
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

package amqptest

import (
	"os"
	"testing"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/amqpbus"
	"github.com/livekit/psrpc/pkg/bus/bustest"
)

func init() {
	bustest.RegisterServer("RabbitMQ", New)
}

// New connects to an external RabbitMQ instance pointed to by AMQP_URL
// (e.g. amqp://guest:guest@localhost:5672/ or amqps://user:pass@host/vhost).
// The suite skips this server when AMQP_URL is not set. Spinning up
// RabbitMQ through dockertest is deliberately not attempted: its image
// startup time is several times the whole suite's, and CI can point
// AMQP_URL at a service container instead.
func New(t testing.TB) bustest.Server {
	url := os.Getenv("AMQP_URL")
	if url == "" {
		t.Skip("AMQP_URL not set; skipping RabbitMQ conformance tests")
	}
	return &rabbitMQServer{url: url}
}

type rabbitMQServer struct {
	url string
}

func (s *rabbitMQServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	b, err := amqpbus.New(s.url, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}
