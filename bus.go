package psrpc

import (
	"context"

	"google.golang.org/protobuf/proto"
)

const (
	DefaultChannelSize = 100
)

type MessageBus interface {
	Publish(ctx context.Context, channel string, msg proto.Message) error
	Subscribe(ctx context.Context, channel string, channelSize int) (subInternal, error)
	SubscribeQueue(ctx context.Context, channel string, channelSize int) (subInternal, error)
}

type subInternal interface {
	read() ([]byte, bool)
	Close() error
}

type Subscription[MessageType proto.Message] interface {
	Channel() <-chan MessageType
	Close() error
}

type subscription[MessageType proto.Message] struct {
	subInternal
	c <-chan MessageType
}

func (s *subscription[MessageType]) Channel() <-chan MessageType {
	return s.c
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

	return toSubscription[MessageType](sub, channelSize), nil
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

	return toSubscription[MessageType](sub, channelSize), nil
}

func toSubscription[MessageType proto.Message](sub subInternal, size int) Subscription[MessageType] {
	msgChan := make(chan MessageType, size)
	go func() {
		for {
			b, ok := sub.read()
			if !ok {
				close(msgChan)
				return
			}

			p, err := deserialize(b)
			if err != nil {
				logger.Error(err, "failed to deserialize message")
				continue
			}
			msgChan <- p.(MessageType)
		}
	}()

	return &subscription[MessageType]{
		subInternal: sub,
		c:           msgChan,
	}
}
