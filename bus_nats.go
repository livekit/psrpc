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
	return &natsMessageBus{
		nc: nc,
	}
}

func (n *natsMessageBus) Publish(ctx context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return n.nc.Publish(channel, b)
}

func (n *natsMessageBus) Subscribe(ctx context.Context, channel string, size int) (subInternal, error) {
	msgChan := make(chan *nats.Msg, size)
	sub, err := n.nc.ChanSubscribe(channel, msgChan)
	if err != nil {
		return nil, err
	}

	return &natsSubscription{
		sub:     sub,
		msgChan: msgChan,
	}, nil
}

func (n *natsMessageBus) SubscribeQueue(ctx context.Context, channel string, size int) (subInternal, error) {
	msgChan := make(chan *nats.Msg, size)
	sub, err := n.nc.ChanQueueSubscribe(channel, "bus", msgChan)
	if err != nil {
		return nil, err
	}

	return &natsSubscription{
		sub:     sub,
		msgChan: msgChan,
	}, nil
}

type natsSubscription struct {
	sub     *nats.Subscription
	msgChan chan *nats.Msg
}

func (n *natsSubscription) read() ([]byte, bool) {
	msg, ok := <-n.msgChan
	if !ok {
		return nil, false
	}
	return msg.Data, true
}

func (n *natsSubscription) Close() error {
	return n.sub.Unsubscribe()
}
