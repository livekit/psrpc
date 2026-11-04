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

package mqtttest

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/bustest"
	"github.com/livekit/psrpc/pkg/bus/bustest/dockerutil"
	"github.com/livekit/psrpc/pkg/bus/mqttbus"
	"github.com/livekit/psrpc/pkg/rand"
)

func init() {
	bustest.RegisterServer("MQTT", New)
}

var mqttLast = uint32(0)

// New starts a Mosquitto broker (or connects to an external one pointed to
// by MQTT_URL, e.g. tcp://localhost:1883) for the bus conformance suite.
// When neither is available the server is skipped. The broker must support
// MQTT 5 shared subscriptions ($share).
func New(t testing.TB) bustest.Server {
	if url := os.Getenv("MQTT_URL"); url != "" {
		return &mqttServer{url: url}
	}

	ctx := context.Background()
	pool := dockerutil.TryPool()
	if pool == nil {
		t.Skip("no docker available for Mosquitto (set MQTT_URL to test against an external broker)")
	}
	c, err := pool.Run(ctx, "eclipse-mosquitto",
		dockertest.WithTag("2"),
		dockertest.WithCmd([]string{"mosquitto", "-c", "/mosquitto-no-auth.conf"}),
		dockertest.WithName(fmt.Sprintf("psrpc-mqtt-%d", atomic.AddUint32(&mqttLast, 1))),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(ctx)
	})
	addr := c.GetHostPort("1883/tcp")
	url := "tcp://" + addr
	dockerutil.WaitTCPPort(t, pool, addr)
	t.Log("Mosquitto running on", addr)

	return &mqttServer{url: url}
}

type mqttServer struct {
	url string
}

func (s *mqttServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	// A unique client id per bus: several buses are alive concurrently in one
	// test (server + client), and a broker disconnects an existing session
	// when a new connection reuses its client id — the buses would flap each
	// other's connections forever.
	ctx := t.Context()
	b, err := mqttbus.New(ctx, s.url, "psrpc-bustest-"+rand.NewString(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}
