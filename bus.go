package psrpc

import (
	"context"
	"errors"

	"github.com/go-redis/redis/v8"
	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

type busType string

const (
	redisBus busType = "redis"
	natsBus  busType = "nats"
)

type MessageBus interface {
	getBusType() busType
	getRC() redis.UniversalClient
	getNC() *nats.Conn
}

type Subscription[MessageType proto.Message] interface {
	Channel() <-chan MessageType
	Close() error
}

func Publish(bus MessageBus, ctx context.Context, channel string, msg proto.Message) error {
	switch bus.getBusType() {
	case redisBus:
		return redisPublish(bus.getRC(), ctx, channel, msg)
	case natsBus:
		return natsPublish(bus.getNC(), ctx, channel, msg)
	default:
		return errors.New("not connected")
	}
}

func Subscribe[MessageType proto.Message](bus MessageBus, ctx context.Context, channel string) (Subscription[MessageType], error) {
	switch bus.getBusType() {
	case redisBus:
		return redisSubscribe[MessageType](bus.getRC(), ctx, channel)
	case natsBus:
		return natsSubscribe[MessageType](bus.getNC(), ctx, channel)
	default:
		return nil, errors.New("not connected")
	}
}

func SubscribeQueue[MessageType proto.Message](bus MessageBus, ctx context.Context, channel string) (Subscription[MessageType], error) {
	switch bus.getBusType() {
	case redisBus:
		return redisSubscribeQueue[MessageType](bus.getRC(), ctx, channel)
	case natsBus:
		return natsSubscribeQueue[MessageType](bus.getNC(), ctx, channel)
	default:
		return nil, errors.New("not connected")
	}
}

type bus struct {
	MessageBus
	busType

	rc redis.UniversalClient
	nc *nats.Conn
}

func (b *bus) getBusType() busType {
	return b.busType
}

func (b *bus) getRC() redis.UniversalClient {
	return b.rc
}

func (b *bus) getNC() *nats.Conn {
	return b.nc
}
