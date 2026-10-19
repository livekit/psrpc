package bus_test

import (
	"context"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

// TestAmqpChannelNameMapping exercises channel names shaped like real PSRPC
// channels. The conformance suite uses plain random strings, so the
// '|'-to-'.' mapping, unicode escaping and the 255-byte hash fallback are
// not covered by TestMessageBus.
func TestAmqpChannelNameMapping(t *testing.T) {
	url := os.Getenv("AMQP_URL")
	if url == "" {
		t.Skip("AMQP_URL not set; skipping RabbitMQ channel name mapping test")
	}

	b, err := bus.NewAmqpMessageBus(url)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	// psrpc escapes each non-[0-9A-Za-z_] rune to 6 bytes, so a non-Latin
	// room name produces a channel far beyond the 255-byte shortstr limit.
	unicodeTopic := strings.Repeat("会议室", 60)
	for _, channel := range []bus.Channel{
		{Legacy: "livekit|" + rand.NewString() + "|egress|REQ"},
		{Legacy: "livekit|" + unicodeTopic + "|REQ"},
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

// TestAmqpReconnection severs the bus's connection mid-flight and expects the
// bus to re-dial and both existing and newly created subscriptions to keep
// receiving afterwards. Subscribing while the broker connection is down must
// succeed like it does on the Redis bus, not fail.
func TestAmqpReconnection(t *testing.T) {
	amqpURL := os.Getenv("AMQP_URL")
	if amqpURL == "" {
		t.Skip("AMQP_URL not set; skipping RabbitMQ reconnection test")
	}
	u, err := url.Parse(amqpURL)
	require.NoError(t, err)
	if u.Port() == "" {
		port := "5672"
		if u.Scheme == "amqps" {
			port = "5671"
		}
		u.Host = net.JoinHostPort(u.Hostname(), port)
	}
	proxy := startTCPProxy(t, u.Host)
	u.Host = proxy.addr()

	b, err := bus.NewAmqpMessageBus(u.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	channel := bus.Channel{Legacy: "livekit|" + rand.NewString() + "|REQ"}
	ctx := context.Background()
	sub, err := bus.Subscribe[*internal.Request](ctx, b, channel, bus.DefaultChannelSize)
	require.NoError(t, err)

	// Sanity: delivery works before the connection is severed.
	require.NoError(t, b.Publish(ctx, channel, &internal.Request{RequestId: "before"}))
	waitForRequest(t, sub, "before")

	proxy.sever()
	time.Sleep(time.Millisecond * 200)

	// Subscribing during the outage is accepted and delivers once the bus
	// has re-dialed.
	channel2 := bus.Channel{Legacy: "livekit|" + rand.NewString() + "|REQ"}
	sub2, err := bus.Subscribe[*internal.Request](ctx, b, channel2, bus.DefaultChannelSize)
	require.NoError(t, err)

	var published bool
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
		if err = b.Publish(ctx, channel, &internal.Request{RequestId: "after"}); err == nil {
			published = true
			break
		}
		time.Sleep(time.Millisecond * 200)
	}
	require.True(t, published, "publish never succeeded after reconnection")
	waitForRequest(t, sub, "after")

	require.NoError(t, b.Publish(ctx, channel2, &internal.Request{RequestId: "joined"}))
	waitForRequest(t, sub2, "joined")
}
