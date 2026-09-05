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

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

// TestAmqpChannelNameMapping exercises channel names shaped like real PSRPC
// channels, which contain '|'. The conformance suite uses plain random
// strings, so the '|'-to-'.' mapping is not covered by TestMessageBus.
func TestAmqpChannelNameMapping(t *testing.T) {
	url := os.Getenv("AMQP_URL")
	if url == "" {
		t.Skip("AMQP_URL not set; skipping RabbitMQ channel name mapping test")
	}

	b, err := bus.NewAmqpMessageBus(url)
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

// tcpProxy is a minimal L4 proxy in front of the broker, used to sever the
// bus's connection without touching the broker itself. The listener stays
// open across a sever so the bus re-dials through the proxy afterwards.
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

// TestAmqpReconnection severs the bus's connection mid-flight and expects the
// bus to re-dial and the subscription to keep receiving afterwards.
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
