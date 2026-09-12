package bustest

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("MQTT", NewMQTT)
}

var mqttLast = baseID

// NewMQTT starts a Mosquitto broker (or connects to an external one pointed
// to by MQTT_URL, e.g. tcp://localhost:1883) for the bus conformance suite.
// The broker must support MQTT 5 shared subscriptions ($share).
func NewMQTT(t testing.TB, pool dockertest.Pool) Server {
	if url := os.Getenv("MQTT_URL"); url != "" {
		return &mqttServer{url: url}
	}

	ctx := context.Background()
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
	t.Log("Mosquitto running on", addr)

	return &mqttServer{url: url}
}

type mqttServer struct {
	url string
}

func (s *mqttServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	b, err := bus.NewMqttMessageBus(s.url, "psrpc-bustest", opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}