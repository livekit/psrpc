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
	"slices"
	"sync"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"
)

type natsMessageBus struct {
	nc *nats.Conn

	mu      sync.Mutex
	routers map[string]*natsRouter
}

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return &natsMessageBus{
		nc:      nc,
		routers: map[string]*natsRouter{},
	}
}

func (n *natsMessageBus) Publish(_ context.Context, channel Channel, msg proto.Message) error {
	b, err := serialize(msg, channel.Local)
	if err != nil {
		return err
	}
	return n.nc.Publish(channel.Server, b)
}

func (n *natsMessageBus) Subscribe(_ context.Context, channel Channel, size int) (Reader, error) {
	if channel.Local == "" {
		return n.subscribe(channel.Server, size, false)
	} else {
		return n.subscribeRouter(channel, size, false)
	}
}

func (n *natsMessageBus) SubscribeQueue(_ context.Context, channel Channel, size int) (Reader, error) {
	if channel.Local == "" {
		return n.subscribe(channel.Server, size, true)
	} else {
		return n.subscribeRouter(channel, size, true)
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

func (n *natsMessageBus) unsubscribeRouter(r *natsRouter, channel string, s *natsRouterSubscription) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.close(channel, s) {
		delete(n.routers, r.channel)
	}
}

func (n *natsMessageBus) subscribeRouter(channel Channel, size int, queue bool) (*natsRouterSubscription, error) {
	sub := &natsRouterSubscription{
		msgChan: make(chan *nats.Msg, size),
		channel: channel.Local,
	}

	n.mu.Lock()
	r, ok := n.routers[channel.Server]
	if !ok {
		r = &natsRouter{
			routes:  map[string][]*natsRouterSubscription{},
			bus:     n,
			channel: channel.Server,
			queue:   queue,
		}
		n.routers[channel.Server] = r
	} else if r.queue != queue {
		n.mu.Unlock()
		return nil, fmt.Errorf("subscription type mismatch for channel %q %q", channel, sub.channel)
	}

	r.open(sub.channel, sub)
	sub.router = r
	n.mu.Unlock()

	if ok {
		return sub, nil
	}

	var err error
	if queue {
		r.sub, err = n.nc.QueueSubscribe(channel.Server, "bus", r.write)
	} else {
		r.sub, err = n.nc.Subscribe(channel.Server, r.write)
	}
	if err != nil {
		n.mu.Lock()
		delete(n.routers, channel.Server)
		n.mu.Unlock()
		return nil, err
	}

	return sub, nil
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

type natsRouter struct {
	sub     *nats.Subscription
	mu      sync.Mutex
	routes  map[string][]*natsRouterSubscription
	bus     *natsMessageBus
	channel string
	queue   bool
}

func (n *natsRouter) open(channel string, s *natsRouterSubscription) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routes[channel] = append(n.routes[channel], s)
}

func (n *natsRouter) close(channel string, s *natsRouterSubscription) bool {
	n.mu.Lock()
	defer n.mu.Unlock()

	subs := n.routes[channel]
	i := slices.Index(n.routes[channel], s)
	if i == -1 {
		return false
	}

	if len(subs) > 1 {
		n.routes[channel] = slices.Delete(subs, i, i+1)
		return false
	}

	delete(n.routes, channel)
	if len(n.routes) == 0 {
		n.sub.Unsubscribe()
		return true
	}
	return false
}

func (n *natsRouter) write(m *nats.Msg) {
	channel, err := deserializeChannel(m.Data)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.routes[channel] {
		s.write(m)
	}
}

type natsRouterSubscription struct {
	msgChan chan *nats.Msg
	router  *natsRouter
	channel string
}

func (n *natsRouterSubscription) write(m *nats.Msg) {
	n.msgChan <- m
}

func (n *natsRouterSubscription) read() ([]byte, bool) {
	msg, ok := <-n.msgChan
	if !ok {
		return nil, false
	}
	return msg.Data, true
}

func (n *natsRouterSubscription) Close() error {
	n.router.bus.unsubscribeRouter(n.router, n.channel, n)
	close(n.msgChan)
	return nil
}
