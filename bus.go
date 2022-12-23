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
	redisBus    busType = "redis"
	natsBus     busType = "nats"
	channelSize         = 100
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

var ErrBusNotConnected = errors.New("bus not connected")

func Publish(ctx context.Context, bus MessageBus, channel string, msg proto.Message) error {
	switch bus.getBusType() {
	case redisBus:
		return redisPublish(bus.getRC(), ctx, channel, msg)
	case natsBus:
		return natsPublish(bus.getNC(), ctx, channel, msg)
	default:
		return ErrBusNotConnected
	}
}

func Subscribe[MessageType proto.Message](ctx context.Context, bus MessageBus, channel string) (Subscription[MessageType], error) {
	switch bus.getBusType() {
	case redisBus:
		return redisSubscribe[MessageType](bus.getRC(), ctx, channel)
	case natsBus:
		return natsSubscribe[MessageType](bus.getNC(), ctx, channel)
	default:
		return nil, ErrBusNotConnected
	}
}

func SubscribeQueue[MessageType proto.Message](ctx context.Context, bus MessageBus, channel string) (Subscription[MessageType], error) {
	switch bus.getBusType() {
	case redisBus:
		return redisSubscribeQueue[MessageType](bus.getRC(), ctx, channel)
	case natsBus:
		return natsSubscribeQueue[MessageType](bus.getNC(), ctx, channel)
	default:
		return nil, ErrBusNotConnected
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
