// Package redistest registers a Docker-backed Redis broker with bustest. Import it
// for side effects.
package redistest

import (
	"context"
	"testing"
	"time"

	"github.com/ory/dockertest/v4"
	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/bustest"
	"github.com/livekit/psrpc/pkg/bus/bustest/dockerutil"
	"github.com/livekit/psrpc/pkg/bus/redisbus"
)

func init() {
	bustest.RegisterServer("Redis", New)
}

func New(t testing.TB) bustest.Server {
	ctx := context.Background()
	pool := dockerutil.Pool(t)
	c, err := pool.Run(ctx, "redis",
		dockertest.WithTag("latest"),
		dockertest.WithName(dockerutil.Name("redis")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	addr := c.GetHostPort("6379/tcp")
	dockerutil.WaitTCPPort(t, pool, addr)

	t.Log("Redis running on", addr)

	s := &redisServer{addr: addr}

	err = pool.Retry(ctx, 0, func() error {
		rc, err := s.connect()
		if err != nil {
			return err
		}
		_ = rc.Close()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return s
}

type redisServer struct {
	addr string
}

func (s *redisServer) connect() (redis.UniversalClient, error) {
	rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{s.addr}})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := rc.Ping(ctx).Err(); err != nil {
		_ = rc.Close()
		return nil, err
	}

	return rc, nil
}

func (s *redisServer) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	rc, err := s.connect()
	if err != nil {
		t.Fatal(err)
	}
	return redisbus.New(rc, opts...)
}
