// Package natstest registers a Docker-backed NATS broker with bustest. Import it
// for side effects.
package natstest

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/bustest"
	"github.com/livekit/psrpc/pkg/bus/bustest/dockerutil"
	"github.com/livekit/psrpc/pkg/bus/natsbus"
)

func init() {
	bustest.RegisterServer("NATS", New)
}

func New(t testing.TB) bustest.Server {
	ctx := context.Background()
	pool := dockerutil.Pool(t)
	c, err := pool.Run(ctx, "nats",
		dockertest.WithTag("latest"),
		dockertest.WithName(dockerutil.Name("nats")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	addr := c.GetHostPort("4222/tcp")
	dockerutil.WaitTCPPort(t, pool, addr)

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

func (s *natsServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	nc, err := s.connect()
	if err != nil {
		t.Fatal(err)
	}
	return natsbus.New(nc, opts...)
}
