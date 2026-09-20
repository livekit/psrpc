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

// Package natsbus provides a psrpc bus backed by NATS.
package natsbus

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/nats-io/nats.go"

	"github.com/livekit/psrpc/pkg/bus"
)

type transport struct {
	nc *nats.Conn

	mu      sync.Mutex
	routers map[string]*router
}

// nc is borrowed, not owned: closing it is the caller's job.
func New(nc *nats.Conn, opts ...bus.BusOption) bus.MessageBus {
	return bus.New(&transport{
		nc:      nc,
		routers: map[string]*router{},
	}, opts...)
}

func (n *transport) Publish(_ context.Context, channel bus.Channel, b []byte) error {
	return n.nc.Publish(channel.Server, b)
}

func (n *transport) Subscribe(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return n.subscribe(ctx, channel.Server, size, false)
	} else {
		return n.subscribeRouter(ctx, channel, size, false)
	}
}

func (n *transport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return n.subscribe(ctx, channel.Server, size, true)
	} else {
		return n.subscribeRouter(ctx, channel, size, true)
	}
}

func (n *transport) subscribe(ctx context.Context, channel string, size int, queue bool) (*subscription, error) {
	ctx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		ctx:     ctx,
		cancel:  cancel,
		msgChan: make(chan *nats.Msg, size),
	}

	var err error
	if queue {
		sub.sub, err = n.nc.QueueSubscribe(channel, "bus", sub.write)
	} else {
		sub.sub, err = n.nc.Subscribe(channel, sub.write)
	}
	if err != nil {
		return nil, err
	}

	return sub, nil
}

func (n *transport) unsubscribeRouter(r *router, channel string, s *routerSubscription) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if r.close(channel, s) {
		delete(n.routers, r.channel)
	}
}

func (n *transport) subscribeRouter(ctx context.Context, channel bus.Channel, size int, queue bool) (*routerSubscription, error) {
	ctx, cancel := context.WithCancel(ctx)
	sub := &routerSubscription{
		ctx:     ctx,
		cancel:  cancel,
		msgChan: make(chan *nats.Msg, size),
		channel: channel.Local,
	}

	n.mu.Lock()
	r, ok := n.routers[channel.Server]
	if !ok {
		r = &router{
			routes:  map[string][]*routerSubscription{},
			t:       n,
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

type subscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	sub     *nats.Subscription
	msgChan chan *nats.Msg
}

func (n *subscription) write(msg *nats.Msg) {
	select {
	case n.msgChan <- msg:
	case <-n.ctx.Done():
	}
}

func (n *subscription) Read() ([]byte, bool) {
	msg, ok := <-n.msgChan
	if !ok {
		return nil, false
	}
	return msg.Data, true
}

func (n *subscription) Close() error {
	n.cancel()
	err := n.sub.Unsubscribe()
	close(n.msgChan)
	return err
}

type router struct {
	sub     *nats.Subscription
	mu      sync.Mutex
	routes  map[string][]*routerSubscription
	t       *transport
	channel string
	queue   bool
}

func (n *router) open(channel string, s *routerSubscription) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routes[channel] = append(n.routes[channel], s)
}

func (n *router) close(channel string, s *routerSubscription) bool {
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

func (n *router) write(m *nats.Msg) {
	channel, err := bus.DecodeLocalChannel(m.Data)
	if err != nil {
		return
	}

	n.mu.Lock()
	defer n.mu.Unlock()
	for _, s := range n.routes[channel] {
		s.write(m)
	}
}

type routerSubscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan *nats.Msg
	router  *router
	channel string
}

func (n *routerSubscription) write(m *nats.Msg) {
	select {
	case n.msgChan <- m:
	case <-n.ctx.Done():
	}
}

func (n *routerSubscription) Read() ([]byte, bool) {
	msg, ok := <-n.msgChan
	if !ok {
		return nil, false
	}
	return msg.Data, true
}

func (n *routerSubscription) Close() error {
	n.cancel()
	n.router.t.unsubscribeRouter(n.router, n.channel, n)
	close(n.msgChan)
	return nil
}
