package psrpc

import (
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/internal/bus"
)

type MessageBus bus.MessageBus

func NewLocalMessageBus() MessageBus {
	return bus.NewLocalMessageBus()
}

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return bus.NewNatsMessageBus(nc)
}

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	return bus.NewRedisMessageBus(rc)
}
