package bustest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("MQTT", NewMQTT)
}

var mqttLast = baseID

// NewMQTT starts a Mosquitto broker (or connects to an external one pointed
// to by MQTT_URL, e.g. tcp://localhost:1883) for the bus conformance suite.
// The broker must support MQTT shared subscriptions ($share), which Mosquitto
// 2.x and EMQX do even for 3.1.1 clients.
func NewMQTT(t testing.TB, pool dockertest.Pool) Server {
	if url := os.Getenv("MQTT_URL"); url != "" {
		return &mqttServer{url: url}
	}

	ctx := context.Background()
	c, err := pool.Run(ctx, "eclipse-mosquitto",
		dockertest.WithTag("2"),
		// The image's default config only listens on localhost; the
		// shipped no-auth config listens on all interfaces.
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
	// TCP readiness alone is not enough: docker's port proxy accepts
	// connections before mosquitto has bound the port, so retry until a
	// real MQTT CONNECT succeeds.
	url := "tcp://" + addr
	err = pool.Retry(ctx, 0, func() error {
		client := mqtt.NewClient(mqtt.NewClientOptions().
			AddBroker(url).
			SetClientID("psrpc-bustest-probe").
			SetConnectTimeout(time.Second))
		t := client.Connect()
		if !t.WaitTimeout(time.Second*3) || t.Error() != nil {
			err := t.Error()
			client.Disconnect(0)
			if err == nil {
				err = errors.New("connect timed out")
			}
			return err
		}
		client.Disconnect(0)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Log("Mosquitto running on", addr)

	return &mqttServer{url: url}
}

type mqttServer struct {
	url string
}

func (s *mqttServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	b, err := bus.NewMqttMessageBus(func() *mqtt.ClientOptions {
		return mqtt.NewClientOptions().AddBroker(s.url)
	}, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}
