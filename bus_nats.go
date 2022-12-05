package psrpc

import (
	"context"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

type natsMessageBus struct {
	nc *nats.Conn
}

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return &natsMessageBus{nc: nc}
}

func (n *natsMessageBus) Publish(_ context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return n.nc.Publish(channel, b)
}

func (n *natsMessageBus) Subscribe(_ context.Context, channel string) (Subscription, error) {
	msgChan := make(chan *nats.Msg, ChannelSize)
	sub, err := n.nc.ChanSubscribe(channel, msgChan)
	if err != nil {
		return nil, err
	}

	return toNatsSubscription(sub, msgChan), nil
}

func (n *natsMessageBus) SubscribeQueue(_ context.Context, channel string) (Subscription, error) {
	msgChan := make(chan *nats.Msg, ChannelSize)
	sub, err := n.nc.ChanQueueSubscribe(channel, "bus", msgChan)
	if err != nil {
		return nil, err
	}

	return toNatsSubscription(sub, msgChan), nil
}

func toNatsSubscription(sub *nats.Subscription, msgChan chan *nats.Msg) Subscription {
	dataChan := make(chan proto.Message, ChannelSize)
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
			dataChan <- p
		}
	}()

	return &natsSubscription{
		sub: sub,
		c:   dataChan,
	}
}

type natsSubscription struct {
	sub *nats.Subscription
	c   <-chan proto.Message
}

func (n *natsSubscription) Channel() <-chan proto.Message {
	return n.c
}

func (n *natsSubscription) Close() error {
	return n.sub.Unsubscribe()
}
