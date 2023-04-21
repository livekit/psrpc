package bus

import (
	"context"

	"google.golang.org/protobuf/proto"
)

const (
	DefaultChannelSize = 100
)

type MessageBus interface {
	Publish(ctx context.Context, channel string, msg proto.Message) error
	Subscribe(ctx context.Context, channel string, channelSize int) (Reader, error)
	SubscribeQueue(ctx context.Context, channel string, channelSize int) (Reader, error)
}

type Reader interface {
	read() ([]byte, bool)
	Close() error
}

func Subscribe[MessageType proto.Message](
	ctx context.Context,
	bus MessageBus,
	channel string,
	channelSize int,
) (Subscription[MessageType], error) {

	sub, err := bus.Subscribe(ctx, channel, channelSize)
	if err != nil {
		return nil, err
	}

	return newSubscription[MessageType](sub, channelSize), nil
}

func SubscribeQueue[MessageType proto.Message](
	ctx context.Context,
	bus MessageBus,
	channel string,
	channelSize int,
) (Subscription[MessageType], error) {

	sub, err := bus.SubscribeQueue(ctx, channel, channelSize)
	if err != nil {
		return nil, err
	}

	return newSubscription[MessageType](sub, channelSize), nil
}
