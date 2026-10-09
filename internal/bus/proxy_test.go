package bus_test

import (
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
)

// tcpProxy is a minimal L4 proxy in front of the broker, used to sever a
// bus's connections without touching the broker itself. The listener stays
// open across a sever so the bus can re-dial through the proxy afterwards.
type tcpProxy struct {
	target string

	mu    sync.Mutex
	ln    net.Listener
	conns []net.Conn
}

func startTCPProxy(t *testing.T, target string) *tcpProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
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
