package bustest

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ory/dockertest/v4"
	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("Redis", NewRedis)
}

var redisLast = baseID

func NewRedis(t testing.TB, pool dockertest.Pool) Server {
	ctx := context.Background()
	c, err := pool.Run(ctx, "redis",
		dockertest.WithTag("latest"),
		dockertest.WithName(fmt.Sprintf("psrpc-redis-%d", atomic.AddUint32(&redisLast, 1))),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = c.Close(context.Background())
	})
	addr := c.GetHostPort("6379/tcp")
	waitTCPPort(t, pool, addr)

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

func (s *redisServer) Connect(t testing.TB) bus.MessageBus {
	rc, err := s.connect()
	if err != nil {
		t.Fatal(err)
	}
	return bus.NewRedisMessageBus(rc)
}
