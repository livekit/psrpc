// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bus

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/nats-io/nats.go"
	"go.uber.org/multierr"
	"google.golang.org/protobuf/proto"
)

type natsMessageBus struct {
	nc *nats.Conn

	mu      sync.Mutex
	routers map[string]*natsWildcardRouter
}

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return &natsMessageBus{
		nc:      nc,
		routers: map[string]*natsWildcardRouter{},
	}
}

func (n *natsMessageBus) Publish(_ context.Context, channel Channel, msg proto.Message) error {
	b, err := serialize(msg, channel.Local)
	if err != nil {
		return err
	}

	if ChannelMode.Load() != RouterSubWildcardPub {
		err = multierr.Append(err, n.nc.Publish(channel.Legacy, b))
	}
	if ChannelMode.Load() != LegacySubLegacyPub {
		err = multierr.Append(err, n.nc.Publish(channel.Server, b))
	}
	return err
}

func (n *natsMessageBus) Subscribe(_ context.Context, channel Channel, size int) (Reader, error) {
	if channel.Local == "" {
		if ChannelMode.Load() == RouterSubWildcardPub {
			return n.subscribe(channel.Server, size, false)
		} else {
			return n.subscribeCompatible(channel, size, false)
		}
	} else {
		if ChannelMode.Load() == RouterSubWildcardPub {
			return n.subscribeRouter(channel, size, false)
		} else {
			return n.subscribeCompatibleRouter(channel, size, false)
		}
	}
}

func (n *natsMessageBus) SubscribeQueue(_ context.Context, channel Channel, size int) (Reader, error) {
	if channel.Local == "" {
		if ChannelMode.Load() == RouterSubWildcardPub {
			return n.subscribe(channel.Server, size, true)
		} else {
			return n.subscribeCompatible(channel, size, true)
		}
	} else {
		if ChannelMode.Load() == RouterSubWildcardPub {
			return n.subscribeRouter(channel, size, true)
		} else {
			return n.subscribeCompatibleRouter(channel, size, true)
		}
	}
}

func (n *natsMessageBus) subscribe(channel string, size int, queue bool) (*natsSubscription, error) {
	msgChan := make(chan *nats.Msg, size)
	var sub *nats.Subscription
	var err error
	if queue {
		sub, err = n.nc.ChanQueueSubscribe(channel, "bus", msgChan)
	} else {
		sub, err = n.nc.ChanSubscribe(channel, msgChan)
	}
	if err != nil {
		return nil, err
	}

	return &natsSubscription{
		sub:     sub,
		msgChan: msgChan,
	}, nil
}

func (n *natsMessageBus) subscribeCompatible(channel Channel, size int, queue bool) (*natsCompatibleSubscription, error) {
	sub, err := n.subscribe(channel.Server, size, queue)
	if err != nil {
		return nil, err
	}
	legacySub, err := n.subscribe(channel.Legacy, size, queue)
	if err != nil {
		sub.Close()
		return nil, err
	}

	return &natsCompatibleSubscription{
		sub:           sub,
		legacySub:     legacySub,
		msgChan:       sub.msgChan,
		legacyMsgChan: legacySub.msgChan,
	}, nil
}

func (n *natsMessageBus) subscribeWildcardRouter(channel string, sub *natsWildcardSubscription, queue bool) error {
	n.mu.Lock()
	r, ok := n.routers[channel]
	if !ok {
		r = &natsWildcardRouter{
			routes:  map[string]*natsWildcardSubscription{},
			bus:     n,
			channel: channel,
			queue:   queue,
		}
		n.routers[channel] = r
	} else if r.queue != queue {
		n.mu.Unlock()
		return fmt.Errorf("subscription type mismatch for channel %q %q", channel, sub.channel)
	}

	r.open(sub.channel, sub)
	sub.router = r
	n.mu.Unlock()

	if ok {
		return nil
	}

	var err error
	if queue {
		r.sub, err = n.nc.QueueSubscribe(channel, "bus", r.write)
	} else {
		r.sub, err = n.nc.Subscribe(channel, r.write)
	}
	if err != nil {
		n.mu.Lock()
		delete(n.routers, channel)
		n.mu.Unlock()
	}
	return err
}

func (n *natsMessageBus) unsubscribeWildcardRouter(r *natsWildcardRouter, channel string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.close(channel) {
		delete(n.routers, r.channel)
	}
}

func (n *natsMessageBus) subscribeRouter(channel Channel, size int, queue bool) (*natsWildcardSubscription, error) {
	sub := &natsWildcardSubscription{
		msgChan: make(chan *nats.Msg, size),
		channel: channel.Local,
	}

	if err := n.subscribeWildcardRouter(channel.Server, sub, queue); err != nil {
		return nil, err
	}

	return sub, nil
}

func (n *natsMessageBus) subscribeCompatibleRouter(channel Channel, size int, queue bool) (*natsCompatibleSubscription, error) {
	sub, err := n.subscribeRouter(channel, size, queue)
	if err != nil {
		return nil, err
	}
	legacySub, err := n.subscribe(channel.Legacy, size, queue)
	if err != nil {
		sub.Close()
		return nil, err
	}

	return &natsCompatibleSubscription{
		sub:           sub,
		legacySub:     legacySub,
		msgChan:       sub.msgChan,
		legacyMsgChan: legacySub.msgChan,
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
	err := n.sub.Unsubscribe()
	close(n.msgChan)
	return err
}

type natsWildcardRouter struct {
	sub     *nats.Subscription
	mu      sync.Mutex
	routes  map[string]*natsWildcardSubscription
	bus     *natsMessageBus
	channel string
	queue   bool
}

func (n *natsWildcardRouter) open(channel string, s *natsWildcardSubscription) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routes[channel] = s
}

func (n *natsWildcardRouter) close(channel string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.routes, channel)
	if len(n.routes) == 0 {
		n.sub.Unsubscribe()
		return true
	}
	return false
}

func (n *natsWildcardRouter) write(m *nats.Msg) {
	channel, err := deserializeChannel(m.Data)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	if s, ok := n.routes[channel]; ok {
		s.write(m)
	}
}

type natsWildcardSubscription struct {
	msgChan chan *nats.Msg
	router  *natsWildcardRouter
	channel string
}

func (n *natsWildcardSubscription) write(m *nats.Msg) {
	select {
	case n.msgChan <- m:
	default:
	}
}

func (n *natsWildcardSubscription) read() ([]byte, bool) {
	msg, ok := <-n.msgChan
	if !ok {
		return nil, false
	}
	return msg.Data, true
}

func (n *natsWildcardSubscription) Close() error {
	n.router.bus.unsubscribeWildcardRouter(n.router, n.channel)
	close(n.msgChan)
	return nil
}

type natsCompatibleSubscription struct {
	sub, legacySub         io.Closer
	msgChan, legacyMsgChan chan *nats.Msg
}

func (n *natsCompatibleSubscription) read() ([]byte, bool) {
	for {
		select {
		case msg, ok := <-n.msgChan:
			if !ok {
				return nil, false
			}
			switch ChannelMode.Load() {
			case RouterSubCompatiblePub, RouterSubWildcardPub:
				return msg.Data, true
			}

		case msg, ok := <-n.legacyMsgChan:
			if !ok {
				return nil, false
			}
			switch ChannelMode.Load() {
			case LegacySubLegacyPub, LegacySubCompatiblePub:
				return msg.Data, true
			}
		}
	}
}

func (n *natsCompatibleSubscription) Close() error {
	return multierr.Combine(n.sub.Close(), n.legacySub.Close())
}
