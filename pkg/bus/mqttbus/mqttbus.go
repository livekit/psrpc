// Copyright 2025 LiveKit, Inc.
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

// Package mqttbus provides a psrpc bus backed by MQTT 5.
package mqttbus

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"

	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

const (
	mqttQueueGroup     = "psrpc"
	mqttQoS            = byte(0)
	mqttConnectTimeout = 10 * time.Second
	mqttSessionExpiry  = 3600 // seconds: broker retains subs for 1h across disconnects
	mqttKeepAlive      = 10 * time.Second
	// mqttWireTimeout bounds every SUBSCRIBE/UNSUBSCRIBE round trip so a
	// half-dead connection cannot park the caller (and, through wireMu,
	// other wire operations) past the keepalive interval.
	mqttWireTimeout = 5 * time.Second
)

var errClosed = errors.New("psrpc: mqttbus closed")

// transport is a psrpc bus.Transport backed by MQTT 5 (paho.golang/autopaho).
//
// It owns two autopaho.ConnectionManager instances:
//   - broadcast connection: plain subscriptions (fan-out to every local sub)
//   - queue connection: $share subscriptions (competing consumers across nodes)
//
// MQTT 5's SessionExpiryInterval removes the need for manual resubscription
// on reconnect: the broker retains subscriptions across disconnects.
//
// Subscriptions are wired to the broker through a single serialized path
// (wireMu) so their relative order on the wire always matches the order in
// which the local subscriber lists changed; state is re-validated under the
// bus lock after each wire operation, so an UNSUBSCRIBE can never cancel a
// SUBSCRIBE that a newer subscriber list issued for the same topic.
//
// Subscribing while the broker is down succeeds without touching the wire:
// the list is marked unwired and wired by OnConnectionUp, mirroring the
// Redis bus, which accepts subscriptions while disconnected and reconciles
// them later.
//
// Ordered dispatch is guaranteed: paho.golang drains publishPackets from a
// single goroutine in arrival order, matching the Redis/NATS/Local dispatch
// model.
type transport struct {
	sub   *autopaho.ConnectionManager // broadcast
	queue *autopaho.ConnectionManager // shared subscriptions

	connCtx    context.Context
	connCancel context.CancelFunc

	mu      sync.Mutex
	subs    map[string]*subList // broadcast subscribers
	queues  map[string]*subList // shared subscribers
	routers map[string]*router  // Local demux on one Server subject
	closed  bool

	// wireMu serializes broker SUBSCRIBE/UNSUBSCRIBE operations. It is never
	// held while taking mu, so a slow round trip delays only other wire
	// operations — never Publish or message dispatch.
	wireMu sync.Mutex
}

// New connects to an MQTT 5 broker. brokerURL is a URL like
// tcp://localhost:1883. The broker must support MQTT 5 shared
// subscriptions ($share). The returned bus must be closed by the caller.
func New(ctx context.Context, brokerURL string, clientID string, opts ...bus.BusOption) (*Bus, error) {
	ctx, cancel := context.WithCancel(ctx)

	b := &transport{
		subs:       map[string]*subList{},
		queues:     map[string]*subList{},
		routers:    map[string]*router{},
		connCtx:    ctx,
		connCancel: cancel,
	}

	if clientID == "" {
		clientID = "psrpc-" + rand.NewString()
	}

	parsed, err := url.Parse(brokerURL)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("psrpc: mqttbus: parse broker URL: %w", err)
	}

	// start connects one managed connection. The publish-received handler is
	// set in the client config: autopaho builds every new paho client (one
	// per reconnect) from this config, so config-time handlers survive
	// reconnects without re-registration.
	start := func(suffix string, queue bool) (*autopaho.ConnectionManager, error) {
		subCfg := autopaho.ClientConfig{
			BrokerUrls:                    []*url.URL{parsed},
			KeepAlive:                     uint16(mqttKeepAlive.Seconds()),
			ConnectTimeout:                mqttConnectTimeout,
			CleanStartOnInitialConnection: true,
			SessionExpiryInterval:         mqttSessionExpiry,
			ClientConfig: paho.ClientConfig{
				ClientID: clientID + suffix,
				OnPublishReceived: []func(paho.PublishReceived) (bool, error){
					func(pr paho.PublishReceived) (bool, error) {
						b.dispatch(pr.Packet.Topic, pr.Packet.Payload, queue)
						return true, nil
					},
				},
			},
			OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
				// Wire subscriptions that were registered while the broker
				// was unreachable. Async: OnConnectionUp runs on autopaho's
				// connection goroutine. cm is threaded through instead of
				// reading b.sub/b.queue: the first callback can fire while
				// the constructor is still assigning those fields.
				go b.wirePending(cm, queue)
			},
		}
		cm, err := autopaho.NewConnection(ctx, subCfg)
		if err != nil {
			return nil, fmt.Errorf("psrpc: mqttbus connect: %w", err)
		}
		if err := cm.AwaitConnection(ctx); err != nil {
			return nil, fmt.Errorf("psrpc: mqttbus await connection: %w", err)
		}
		return cm, nil
	}

	if b.sub, err = start("-b", false); err != nil {
		cancel()
		return nil, err
	}
	if b.queue, err = start("-q", true); err != nil {
		_ = b.sub.Disconnect(ctx)
		cancel()
		return nil, err
	}

	return &Bus{MessageBus: bus.New(b, opts...), t: b}, nil
}

// Bus is the MessageBus returned by New. The wrapped transport owns the
// broker connections, so closing the bus is the caller's job.
type Bus struct {
	bus.MessageBus
	t *transport
}

func (b *Bus) Close() error {
	return b.t.Close()
}

// topicName maps a psrpc Server subject (dot hierarchy) to an MQTT topic.
// MQTT topics use '/' as the level separator; Server subjects never contain
// '/', so the mapping is injective.
func topicName(subject string) string {
	return strings.ReplaceAll(subject, ".", "/")
}

func queueFilter(topic string) string {
	return "$share/" + mqttQueueGroup + "/" + topic
}

// dispatch routes an inbound publish to the local subscriber list, if any.
// It is the config-time handler of the broadcast (queue=false) and queue
// (queue=true) connections. The list is dispatched outside mu so a full
// subscriber channel never blocks the bus.
func (b *transport) dispatch(topic string, payload []byte, queue bool) {
	b.mu.Lock()
	lists := b.subs
	if queue {
		lists = b.queues
	}
	list := lists[topic]
	r := b.routers[topic]
	b.mu.Unlock()

	if list != nil {
		if queue {
			list.dispatchQueue(payload)
		} else {
			list.dispatch(payload)
		}
	}
	if r != nil {
		r.write(payload)
	}
}

// ---- bus.Transport interface ----

func (b *transport) Publish(ctx context.Context, channel bus.Channel, payload []byte) error {
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errClosed
	}

	_, err := b.sub.Publish(ctx, &paho.Publish{
		Topic:   topicName(channel.Server),
		QoS:     mqttQoS,
		Payload: payload,
	})
	return err
}

func (b *transport) Subscribe(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return b.subscribe(ctx, topicName(channel.Server), false, size)
	}
	return b.subscribeRouter(ctx, channel, size, false)
}

func (b *transport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return b.subscribe(ctx, topicName(channel.Server), true, size)
	}
	return b.subscribeRouter(ctx, channel, size, true)
}

// targets resolves the subscriber map, managed connection and wire filter for
// a topic.
func (b *transport) targets(topic string, queue bool) (map[string]*subList, *autopaho.ConnectionManager, string) {
	if queue {
		return b.queues, b.queue, queueFilter(topic)
	}
	return b.subs, b.sub, topic
}

func (b *transport) subscribe(ctx context.Context, topic string, queue bool, size int) (bus.Reader, error) {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &subscription{
		bus:    b,
		ctx:    subCtx,
		cancel: cancel,
		topic:  topic,
		queue:  queue,
		ch:     make(chan []byte, size),
	}

	lists, cm, _ := b.targets(topic, queue)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return nil, errClosed
	}
	list, ok := lists[topic]
	if !ok {
		list = &subList{}
		lists[topic] = list
	}
	list.add(sub)
	b.mu.Unlock()

	if err := b.wireTopic(cm, topic, queue); err != nil {
		// Roll back our registration; the list is dropped when it drains.
		b.mu.Lock()
		if cur := lists[topic]; cur == list {
			list.remove(sub)
			if list.empty() {
				delete(lists, topic)
			}
		}
		b.mu.Unlock()
		cancel()
		return nil, err
	}
	return sub, nil
}

// wireTopic subscribes topic on the broker unless it already is. Wire
// operations are serialized by wireMu and re-validate the list under mu after
// acquiring it, which keeps the wire order consistent with subscriber-list
// changes even when they interleave:
//
//   - a subscribe whose list was drained and replaced before it got wireMu
//     finds the replacement list and wires that one (or skips it, if wired);
//   - an unsubscribe whose entry was replaced by a new subscriber list skips
//     its UNSUBSCRIBE instead of cancelling the fresh SUBSCRIBE.
//
// While the broker connection is down the call succeeds without a wire
// effect: autopaho's ConnectionDownError marks the list pending, and
// OnConnectionUp wires it later — mirroring the Redis bus, which accepts
// subscriptions while disconnected.
func (b *transport) wireTopic(cm *autopaho.ConnectionManager, topic string, queue bool) error {
	lists := b.subs
	filter := topic
	if queue {
		lists = b.queues
		filter = queueFilter(topic)
	}

	b.wireMu.Lock()
	defer b.wireMu.Unlock()

	b.mu.Lock()
	list := lists[topic]
	if list == nil || list.wired {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if err := b.wireSubscribe(cm, filter); err != nil {
		if errors.Is(err, autopaho.ConnectionDownError) {
			// Broker connection down: accepted, wired on connection-up.
			return nil
		}
		return fmt.Errorf("psrpc: mqttbus subscribe to %q failed: %w", filter, err)
	}
	b.mu.Lock()
	if cur := lists[topic]; cur == list {
		list.wired = true
	}
	b.mu.Unlock()
	return nil
}

func (b *transport) wireSubscribe(cm *autopaho.ConnectionManager, filter string) error {
	ctx, cancel := context.WithTimeout(b.connCtx, mqttWireTimeout)
	defer cancel()
	// paho validates SUBACK reason codes and errors on failure (>= 0x80).
	_, err := cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: filter, QoS: mqttQoS},
		},
	})
	return err
}

// wirePending wires every list that was registered while the broker was
// unreachable. Called from OnConnectionUp with the manager that came up,
// separately for the broadcast and queue connections.
func (b *transport) wirePending(cm *autopaho.ConnectionManager, queue bool) {
	lists := b.subs
	if queue {
		lists = b.queues
	}
	b.mu.Lock()
	var topics []string
	for t, l := range lists {
		if !l.wired {
			topics = append(topics, t)
		}
	}
	routers := make([]*router, 0, len(b.routers))
	for _, r := range b.routers {
		if !r.wired {
			routers = append(routers, r)
		}
	}
	b.mu.Unlock()

	for _, t := range topics {
		if err := b.wireTopic(cm, t, queue); err != nil {
			// Left unwired; retried on the next connection-up.
			logger.Error(err, "mqttbus subscription wiring failed", "topic", t)
		}
	}
	for _, r := range routers {
		if r.queue != queue {
			continue
		}
		if err := b.wireRouter(cm, r); err != nil {
			logger.Error(err, "mqttbus subscription wiring failed", "topic", r.topic)
		}
	}
}

func (b *transport) unsubscribe(topic string, queue bool, sub *subscription) {
	lists, _, _ := b.targets(topic, queue)

	b.mu.Lock()
	list, ok := lists[topic]
	if !ok {
		b.mu.Unlock()
		return
	}
	if !list.remove(sub) {
		b.mu.Unlock()
		return
	}
	drained := list.empty()
	if drained {
		delete(lists, topic)
	}
	b.mu.Unlock()

	if !drained {
		return
	}

	_, cm, filter := b.targets(topic, queue)
	b.wireMu.Lock()
	defer b.wireMu.Unlock()
	b.mu.Lock()
	_, replaced := lists[topic]
	b.mu.Unlock()
	if replaced {
		// A new subscriber list was registered for this topic while we were
		// draining; its SUBSCRIBE (queued behind wireMu) must not be
		// cancelled by our UNSUBSCRIBE.
		return
	}
	// Best-effort: SessionExpiryInterval means the broker reaps the
	// subscription when the session ends, so a failure (including
	// ConnectionDownError) is safe to ignore.
	ctx, cancel := context.WithTimeout(b.connCtx, mqttWireTimeout)
	defer cancel()
	_, _ = cm.Unsubscribe(ctx, &paho.Unsubscribe{
		Topics: []string{filter},
	})
}

func (b *transport) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subs []*subscription
	for _, l := range b.subs {
		subs = append(subs, l.all()...)
	}
	for _, l := range b.queues {
		subs = append(subs, l.all()...)
	}
	for _, r := range b.routers {
		subs = append(subs, r.all()...)
	}
	b.mu.Unlock()

	b.connCancel()
	for _, s := range subs {
		s.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), mqttWireTimeout)
	defer cancel()
	if b.sub != nil {
		_ = b.sub.Disconnect(ctx)
	}
	if b.queue != nil {
		_ = b.queue.Disconnect(ctx)
	}
	return nil
}

// ---- subList ----

type subList struct {
	mu   sync.Mutex
	subs []*subscription
	next int

	// wired is guarded by the bus mutex, not the list's own: it is read and
	// written by the wire path, which coordinates through the bus lock.
	wired bool
}

func (l *subList) add(sub *subscription) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs = append(l.subs, sub)
}

func (l *subList) remove(sub *subscription) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.subs, sub)
	if i == -1 {
		return false
	}
	l.subs = slices.Delete(l.subs, i, i+1)
	return true
}

func (l *subList) empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.subs) == 0
}

func (l *subList) all() []*subscription {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.subs)
}

func (l *subList) dispatch(payload []byte) {
	l.mu.Lock()
	subs := slices.Clone(l.subs)
	l.mu.Unlock()
	for _, sub := range subs {
		sub.write(payload)
	}
}

func (l *subList) dispatchQueue(payload []byte) {
	l.mu.Lock()
	if len(l.subs) == 0 {
		l.mu.Unlock()
		return
	}
	if l.next >= len(l.subs) {
		l.next = 0
	}
	sub := l.subs[l.next]
	l.next++
	l.mu.Unlock()
	sub.write(payload)
}

// ---- subscription ----

type subscription struct {
	bus    *transport
	ctx    context.Context
	cancel context.CancelFunc
	topic  string
	queue  bool
	ch     chan []byte

	// Set when the subscription is routed through a router (Local demux):
	// Close unwinds the router entry instead of the plain subscriber list.
	r     *router
	local string
}

func (s *subscription) write(payload []byte) {
	select {
	case s.ch <- payload:
	case <-s.ctx.Done():
	}
}

func (s *subscription) Read() ([]byte, bool) {
	select {
	case payload, ok := <-s.ch:
		return payload, ok
	case <-s.ctx.Done():
		return nil, false
	}
}

func (s *subscription) Close() error {
	s.cancel()
	if s.r != nil {
		s.bus.unsubscribeRouter(s.r, s.local, s)
	} else {
		s.bus.unsubscribe(s.topic, s.queue, s)
	}
	return nil
}

// ---- router: Local demux on one Server subject ----

// router fans one broker topic out to per-Local subscriptions, following the
// natsbus pattern: the Server subject is shared by every logical channel, and
// Channel.Local is carried in the payload (bus.DecodeLocalChannel).
type router struct {
	t     *transport
	topic string
	queue bool

	// entries and wired are guarded by the bus mutex: both are mutated from
	// subscribe/unsubscribe paths and the serialized wire path.
	entries map[string][]*subscription // Local channel -> subs
	wired   bool
}

func (b *transport) subscribeRouter(ctx context.Context, channel bus.Channel, size int, queue bool) (bus.Reader, error) {
	subCtx, cancel := context.WithCancel(ctx)
	topic := topicName(channel.Server)
	local := channel.Local
	sub := &subscription{
		bus:    b,
		ctx:    subCtx,
		cancel: cancel,
		topic:  topic,
		queue:  queue,
		local:  local,
		ch:     make(chan []byte, size),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return nil, errClosed
	}
	r, ok := b.routers[topic]
	if !ok {
		r = &router{
			t:      b,
			topic:  topic,
			queue:  queue,
			entries: map[string][]*subscription{},
		}
		b.routers[topic] = r
	} else if r.queue != queue {
		b.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("psrpc: mqttbus subscription type mismatch for topic %q local %q", topic, local)
	}
	r.entries[local] = append(r.entries[local], sub)
	sub.r = r
	b.mu.Unlock()

	if err := b.wireRouter(nil, r); err != nil {
		b.mu.Lock()
		b.removeRouterEntry(r, local, sub)
		b.mu.Unlock()
		cancel()
		return nil, err
	}
	return sub, nil
}

// wireRouter subscribes the router's topic on the broker unless it already is
// wired. cm may be nil outside connection-up: the owning manager is resolved
// from the transport.
func (b *transport) wireRouter(cm *autopaho.ConnectionManager, r *router) error {
	_, defaultCM, filter := b.targets(r.topic, r.queue)
	if cm == nil {
		cm = defaultCM
	}

	b.wireMu.Lock()
	defer b.wireMu.Unlock()

	b.mu.Lock()
	cur, ok := b.routers[r.topic]
	if !ok || cur != r || r.wired {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	if err := b.wireSubscribe(cm, filter); err != nil {
		if errors.Is(err, autopaho.ConnectionDownError) {
			// Broker connection down: accepted, wired on connection-up.
			return nil
		}
		return fmt.Errorf("psrpc: mqttbus subscribe to %q failed: %w", filter, err)
	}
	b.mu.Lock()
	if cur, ok := b.routers[r.topic]; ok && cur == r {
		r.wired = true
	}
	b.mu.Unlock()
	return nil
}

// write demultiplexes one inbound payload by its Local channel.
func (r *router) write(payload []byte) {
	local, err := bus.DecodeLocalChannel(payload)
	if err != nil {
		return
	}
	r.t.mu.Lock()
	subs := slices.Clone(r.entries[local])
	r.t.mu.Unlock()
	for _, sub := range subs {
		sub.write(payload)
	}
}

// removeRouterEntry requires the bus mutex to be held.
func (b *transport) removeRouterEntry(r *router, local string, sub *subscription) {
	subs := r.entries[local]
	i := slices.Index(subs, sub)
	if i == -1 {
		return
	}
	subs = slices.Delete(subs, i, i+1)
	if len(subs) == 0 {
		delete(r.entries, local)
	} else {
		r.entries[local] = subs
	}
	if len(r.entries) == 0 {
		if cur, ok := b.routers[r.topic]; ok && cur == r {
			delete(b.routers, r.topic)
		}
	}
}

func (b *transport) unsubscribeRouter(r *router, local string, sub *subscription) {
	b.mu.Lock()
	cur, ok := b.routers[r.topic]
	if !ok || cur != r {
		b.mu.Unlock()
		return
	}
	b.removeRouterEntry(r, local, sub)
	empty := len(r.entries) == 0
	b.mu.Unlock()

	if !empty {
		return
	}

	_, cm, filter := b.targets(r.topic, r.queue)
	b.wireMu.Lock()
	defer b.wireMu.Unlock()
	b.mu.Lock()
	_, replaced := b.routers[r.topic]
	b.mu.Unlock()
	if replaced {
		// A new router was registered for this topic while we were
		// draining; its SUBSCRIBE (queued behind wireMu) must not be
		// cancelled by our UNSUBSCRIBE.
		return
	}
	// Best-effort: SessionExpiryInterval means the broker reaps the
	// subscription when the session ends.
	ctx, cancel := context.WithTimeout(b.connCtx, mqttWireTimeout)
	defer cancel()
	_, _ = cm.Unsubscribe(ctx, &paho.Unsubscribe{
		Topics: []string{filter},
	})
}

func (r *router) all() []*subscription {
	var subs []*subscription
	for _, s := range r.entries {
		subs = append(subs, s...)
	}
	return subs
}
