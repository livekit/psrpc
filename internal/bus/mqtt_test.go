package bus_test

import (
	"context"
	"io"
	"net"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

// TestMqttChannelNameMapping exercises channel names shaped like real PSRPC
// channels, which contain '|'. The conformance suite uses plain random
// strings, so the '|'-to-'/' mapping is not covered by TestMessageBus.
func TestMqttChannelNameMapping(t *testing.T) {
	url := os.Getenv("MQTT_URL")
	if url == "" {
		t.Skip("MQTT_URL not set; skipping MQTT channel name mapping test")
	}

	b, err := bus.NewMqttMessageBus(func() *mqtt.ClientOptions {
		return mqtt.NewClientOptions().AddBroker(url)
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })

	channel := bus.Channel{Legacy: "livekit|" + rand.NewString() + "|egress|REQ"}
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
		t.Fatal("no delivery on '|' channel")
	}

	require.NoError(t, sub.Close())
}

// TestMqttReconnection severs the bus's connections mid-flight and expects
// paho to re-dial, the OnConnect handler to re-establish subscriptions, and
// the subscription to keep receiving afterwards.
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

	// Short keepalive so the severed connection is noticed quickly.
	b, err := bus.NewMqttMessageBus(func() *mqtt.ClientOptions {
		return mqtt.NewClientOptions().AddBroker(u.String()).SetKeepAlive(time.Second * 2)
	})
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

	// QoS 0 deliveries racing the resubscription can be dropped, so keep
	// publishing until one makes it through.
	deadline := time.Now().Add(15 * time.Second)
	for {
		require.True(t, time.Now().Before(deadline), "no delivery after reconnection")
		if err = b.Publish(ctx, channel, &internal.Request{RequestId: "after"}); err == nil {
			select {
			case m := <-sub.Channel():
				if m != nil && m.RequestId == "after" {
					return
				}
			case <-time.After(time.Millisecond * 500):
			}
		}
		time.Sleep(time.Millisecond * 200)
	}
}

// tcpProxy is a minimal L4 proxy in front of the broker, used to sever the
// bus's connections without touching the broker itself. The listener stays
// open across a sever so paho can re-dial through the proxy afterwards.
type tcpProxy struct {
	target string

	mu    sync.Mutex
	ln    net.Listener
	conns []net.Conn
}

func startTCPProxy(t *testing.T, target string) *tcpProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &tcpProxy{target: target, ln: ln}
	t.Cleanup(p.sever)
	go p.accept()
	return p
}

func (p *tcpProxy) accept() {
	for {
		client, err := p.ln.Accept()
		if err != nil {
			return
		}
		upstream, err := net.Dial("tcp", p.target)
		if err != nil {
			_ = client.Close()
			continue
		}
		p.mu.Lock()
		p.conns = append(p.conns, client, upstream)
		p.mu.Unlock()
		go func() {
			_, _ = io.Copy(upstream, client)
			_ = upstream.Close()
			_ = client.Close()
		}()
		go func() {
			_, _ = io.Copy(client, upstream)
			_ = client.Close()
			_ = upstream.Close()
		}()
	}
}

func (p *tcpProxy) sever() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
}

func (p *tcpProxy) addr() string {
	return p.ln.Addr().String()
}

func waitForRequest(t *testing.T, sub bus.Subscription[*internal.Request], id string) {
	for {
		select {
		case m := <-sub.Channel():
			if m != nil && m.RequestId == id {
				return
			}
		case <-time.After(15 * time.Second):
			t.Fatalf("timed out waiting for request %q", id)
		}
	}
}
