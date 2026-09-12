package bus_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

// TestMqttChannelNameMapping exercises channel names shaped like real PSRPC
// channels. The conformance suite uses plain random strings, so the
// '|'-to-'/' mapping and the escaping of '+' in pkg/info's u+XXXX escapes
// are not covered by TestMessageBus.
func TestMqttChannelNameMapping(t *testing.T) {
	brokerURL := os.Getenv("MQTT_URL")
	if brokerURL == "" {
		t.Skip("MQTT_URL not set; skipping MQTT channel name mapping test")
	}

	b, err := bus.NewMqttMessageBus(brokerURL, "psrpc-test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	for _, channel := range []bus.Channel{
		{Legacy: "livekit|" + rand.NewString() + "|egress|REQ"},
		// what a topic of "room-1" becomes after pkg/info sanitization:
		// the embedded '+' would be an MQTT single-level wildcard
		{Legacy: "livekit|roomu+002d1|REQ"},
		{Legacy: "livekit|u+4f1au+8baeu+5ba4|REQ"},
	} {
		ctx := context.Background()

		sub, err := bus.Subscribe[*internal.Request](ctx, b, channel, bus.DefaultChannelSize)
		require.NoError(t, err)
		time.Sleep(time.Millisecond * 100)

		require.NoError(t, b.Publish(ctx, channel, &internal.Request{RequestId: "42"}))

		select {
		case m := <-sub.Channel():
			require.NotNil(t, m)
			require.Equal(t, "42", m.RequestId)
		case <-time.After(defaultClientTimeout):
			t.Fatalf("no delivery on channel %q", channel.Legacy)
		}

		require.NoError(t, sub.Close())
	}
}

// TestMqttReconnection severs the bus's connections mid-flight and expects
// autopaho to re-dial, subscriptions to persist thanks to SessionExpiryInterval,
// and both existing and newly created subscriptions to keep receiving afterwards.
func TestMqttReconnection(t *testing.T) {
	brokerURL := os.Getenv("MQTT_URL")
	if brokerURL == "" {
		t.Skip("MQTT_URL not set; skipping MQTT reconnection test")
	}
	u, err := url.Parse(brokerURL)
	require.NoError(t, err)
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "1883")
	}
	proxy := startTCPProxy(t, u.Host)
	u.Host = proxy.addr()

	b, err := bus.NewMqttMessageBus(u.String(), "psrpc-test-reconnect")
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	channel := bus.Channel{Legacy: "livekit|" + rand.NewString() + "|REQ"}
	ctx := context.Background()
	sub, err := bus.Subscribe[*internal.Request](ctx, b, channel, bus.DefaultChannelSize)
	require.NoError(t, err)

	// Sanity: delivery works before the connections are severed.
	require.NoError(t, b.Publish(ctx, channel, &internal.Request{RequestId: "before"}))
	waitForRequest(t, sub, "before")

	proxy.sever()
	time.Sleep(time.Millisecond * 500)

	// Subscribing during the outage is accepted and delivers once the bus
	// has reconnected.
	channel2 := bus.Channel{Legacy: "livekit|" + rand.NewString() + "|REQ"}
	sub2, err := bus.Subscribe[*internal.Request](ctx, b, channel2, bus.DefaultChannelSize)
	require.NoError(t, err)

	// QoS 0 deliveries racing reconnection can be dropped.
	deadline := time.Now().Add(15 * time.Second)
	for {
		require.True(t, time.Now().Before(deadline), "no delivery after reconnection")
		if err = b.Publish(ctx, channel, &internal.Request{RequestId: "after"}); err == nil {
			select {
			case m := <-sub.Channel():
				if m != nil && m.RequestId == "after" {
					require.NoError(t, b.Publish(ctx, channel2, &internal.Request{RequestId: "joined"}))
					waitForRequest(t, sub2, "joined")
					return
				}
			case <-time.After(time.Millisecond * 500):
			}
		}
		time.Sleep(time.Millisecond * 200)
	}
}