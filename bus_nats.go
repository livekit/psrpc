package psrpc

import (
	"context"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return &bus{
		busType: natsBus,
		nc:      nc,
	}
}

func natsPublish(nc *nats.Conn, _ context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return nc.Publish(channel, b)
}

func natsSubscribe[MessageType proto.Message](
	nc *nats.Conn, _ context.Context, channel string,
) (Subscription[MessageType], error) {

	msgChan := make(chan *nats.Msg, channelSize)
	sub, err := nc.ChanSubscribe(channel, msgChan)
	if err != nil {
		return nil, err
	}

	return toNatsSubscription[MessageType](sub, msgChan), nil
}

func natsSubscribeQueue[MessageType proto.Message](
	nc *nats.Conn, _ context.Context, channel string,
) (Subscription[MessageType], error) {

	msgChan := make(chan *nats.Msg, channelSize)
	sub, err := nc.ChanQueueSubscribe(channel, "bus", msgChan)
	if err != nil {
		return nil, err
	}

	return toNatsSubscription[MessageType](sub, msgChan), nil
}

func toNatsSubscription[MessageType proto.Message](
	sub *nats.Subscription, msgChan chan *nats.Msg,
) Subscription[MessageType] {

	dataChan := make(chan MessageType, channelSize)
	go func() {
		for {
			msg, ok := <-msgChan
			if !ok {
				close(dataChan)
				return
			}

			p, err := deserialize(msg.Data)
			if err != nil {
				logger.Error(err, "failed to deserialize message")
			}
			dataChan <- p.(MessageType)
		}
	}()

	return &natsSubscription[MessageType]{
		sub: sub,
		c:   dataChan,
	}
}

type natsSubscription[MessageType proto.Message] struct {
	sub *nats.Subscription
	c   <-chan MessageType
}

func (n *natsSubscription[MessageType]) Channel() <-chan MessageType {
	return n.c
}

func (n *natsSubscription[MessageType]) Close() error {
	return n.sub.Unsubscribe()
}
