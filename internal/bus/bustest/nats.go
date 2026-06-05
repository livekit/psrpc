package bustest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("NATS", NewNATS)
}

var natsLast = baseID

func NewNATS(t testing.TB, pool dockertest.Pool) Server {
	ctx := context.Background()
	c, err := pool.Run(ctx, "nats",
		dockertest.WithTag("latest"),
		dockertest.WithName(fmt.Sprintf("psrpc-nats-%d", atomic.AddUint32(&natsLast, 1))),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	addr := c.GetHostPort("4222/tcp")
	waitTCPPort(t, pool, addr)

	t.Log("NATS running on", addr)

	s := &natsServer{addr: "nats://" + addr}

	err = pool.Retry(ctx, 0, func() error {
		nc, err := s.connect()
		if err != nil {
			return err
		}
		nc.Close()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type natsServer struct {
	addr string
}

func (s *natsServer) connect() (*nats.Conn, error) {
	nc, err := nats.Connect(s.addr)
	if err != nil {
		return nil, err
	}
	if err := nc.Flush(); err != nil {
		nc.Close()
		return nil, err
	}
	return nc, nil
}

func (s *natsServer) Connect(t testing.TB) bus.MessageBus {
	nc, err := s.connect()
	if err != nil {
		t.Fatal(err)
	}
	return bus.NewNatsMessageBus(nc)
}
