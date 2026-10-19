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

package bus

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
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/rand"
)

const (
	mqtt5QueueGroup     = "psrpc"
	mqtt5QoS            = byte(0)
	mqtt5ConnectTimeout = 10 * time.Second
	mqtt5SessionExpiry  = 3600 // seconds: broker retains subs for 1h across disconnects
	mqtt5KeepAlive      = 10 * time.Second
	// mqtt5WireTimeout bounds every SUBSCRIBE/UNSUBSCRIBE round trip so a
	// half-dead connection cannot park the caller (and, through wireMu,
	// other wire operations) past the keepalive interval.
	mqtt5WireTimeout = 5 * time.Second
)

var errMqtt5Closed = errors.New("psrpc: mqtt5 message bus closed")

// mqtt5Name maps a psrpc channel to an MQTT topic. Channel parts are
// sanitized to [0-9A-Za-z_], with escape sequences containing '+'.
// '|' → '/' (hierarchical) and '+' → '-' (remove wildcard). The
// replacement is injective because '/' and '-' never occur in the
// legacy alphabet [0-9A-Za-z_|+].
func mqtt5Name(channel Channel) string {
	return strings.NewReplacer("|", "/", "+", "-").Replace(channel.Legacy)
}

func mqtt5QueueFilter(topic string) string {
	return "$share/" + mqtt5QueueGroup + "/" + topic
}

// mqtt5MessageBus is an MQTT 5 MessageBus backed by paho.golang/autopaho.
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
type mqtt5MessageBus struct {
	sub   *autopaho.ConnectionManager // broadcast
	queue *autopaho.ConnectionManager // shared subscriptions

	connCtx    context.Context
	connCancel context.CancelFunc

	mu     sync.Mutex
	subs   map[string]*mqtt5SubList // broadcast subscribers
	queues map[string]*mqtt5SubList // shared subscribers
	closed bool

	// wireMu serializes broker SUBSCRIBE/UNSUBSCRIBE operations. It is never
	// held while taking mu, so a slow round trip delays only other wire
	// operations — never Publish or message dispatch.
	wireMu sync.Mutex

	c       *compressor
	maxSize int
}

// NewMqttMessageBus connects to an MQTT 5 broker. brokerURL is a URL
// like tcp://localhost:1883. The broker must support MQTT 5 shared
// subscriptions ($share). The returned bus must be closed.
func NewMqttMessageBus(brokerURL string, clientID string, opts ...BusOption) (*mqtt5MessageBus, error) {
	o := getBusOpts(opts...)
	ctx, cancel := context.WithCancel(context.Background())

	b := &mqtt5MessageBus{
		subs:       map[string]*mqtt5SubList{},
		queues:     map[string]*mqtt5SubList{},
		connCtx:    ctx,
		connCancel: cancel,
		c:          newCompressor(o.Compression),
		maxSize:    o.Compression.MaxDecompressedSize,
	}

	if clientID == "" {
		clientID = "psrpc-" + rand.NewString()
	}

	parsed, err := url.Parse(brokerURL)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("psrpc: mqtt5: parse broker URL: %w", err)
	}

	// start connects one managed connection. The publish-received handler is
	// set in the client config: autopaho builds every new paho client (one
	// per reconnect) from this config, so config-time handlers survive
	// reconnects without re-registration.
	start := func(suffix string, queue bool) (*autopaho.ConnectionManager, error) {
		subCfg := autopaho.ClientConfig{
			BrokerUrls:                    []*url.URL{parsed},
			KeepAlive:                     uint16(mqtt5KeepAlive.Seconds()),
			ConnectTimeout:                mqtt5ConnectTimeout,
			CleanStartOnInitialConnection: true,
			SessionExpiryInterval:         mqtt5SessionExpiry,
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
			return nil, fmt.Errorf("psrpc: mqtt5 connect: %w", err)
		}
		if err := cm.AwaitConnection(ctx); err != nil {
			return nil, fmt.Errorf("psrpc: mqtt5 await connection: %w", err)
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

	return b, nil
}

// dispatch routes an inbound publish to the local subscriber list, if any.
// It is the config-time handler of the broadcast (queue=false) and queue
// (queue=true) connections. The list is dispatched outside mu so a full
// subscriber channel never blocks the bus.
func (b *mqtt5MessageBus) dispatch(topic string, payload []byte, queue bool) {
	b.mu.Lock()
	lists := b.subs
	if queue {
		lists = b.queues
	}
	list := lists[topic]
	b.mu.Unlock()
	if list == nil {
		return
	}
	if queue {
		list.dispatchQueue(payload)
	} else {
		list.dispatch(payload)
	}
}

func (b *mqtt5MessageBus) maxDecompressedSize() int {
	return b.maxSize
}

// ---- MessageBus interface ----

func (b *mqtt5MessageBus) Publish(ctx context.Context, channel Channel, msg proto.Message) error {
	payload, err := serialize(msg, "", b.c)
	if err != nil {
		return err
	}

	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errMqtt5Closed
	}

	topic := mqtt5Name(channel)
	_, err = b.sub.Publish(ctx, &paho.Publish{
		Topic:   topic,
		QoS:     mqtt5QoS,
		Payload: payload,
	})
	return err
}

func (b *mqtt5MessageBus) Subscribe(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, mqtt5Name(channel), false, size)
}

func (b *mqtt5MessageBus) SubscribeQueue(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, mqtt5Name(channel), true, size)
}

// targets resolves the subscriber map, managed connection and wire filter for
// a topic.
func (b *mqtt5MessageBus) targets(topic string, queue bool) (map[string]*mqtt5SubList, *autopaho.ConnectionManager, string) {
	if queue {
		return b.queues, b.queue, mqtt5QueueFilter(topic)
	}
	return b.subs, b.sub, topic
}

func (b *mqtt5MessageBus) subscribe(ctx context.Context, topic string, queue bool, size int) (Reader, error) {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &mqtt5Subscription{
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
		return nil, errMqtt5Closed
	}
	list, ok := lists[topic]
	if !ok {
		list = &mqtt5SubList{}
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
func (b *mqtt5MessageBus) wireTopic(cm *autopaho.ConnectionManager, topic string, queue bool) error {
	lists := b.subs
	filter := topic
	if queue {
		lists = b.queues
		filter = mqtt5QueueFilter(topic)
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
		return fmt.Errorf("psrpc: mqtt5 subscribe to %q failed: %w", filter, err)
	}
	b.mu.Lock()
	if cur := lists[topic]; cur == list {
		list.wired = true
	}
	b.mu.Unlock()
	return nil
}

func (b *mqtt5MessageBus) wireSubscribe(cm *autopaho.ConnectionManager, filter string) error {
	ctx, cancel := context.WithTimeout(b.connCtx, mqtt5WireTimeout)
	defer cancel()
	// paho validates SUBACK reason codes and errors on failure (>= 0x80).
	_, err := cm.Subscribe(ctx, &paho.Subscribe{
		Subscriptions: []paho.SubscribeOptions{
			{Topic: filter, QoS: mqtt5QoS},
		},
	})
	return err
}

// wirePending wires every list that was registered while the broker was
// unreachable. Called from OnConnectionUp with the manager that came up,
// separately for the broadcast and queue connections.
func (b *mqtt5MessageBus) wirePending(cm *autopaho.ConnectionManager, queue bool) {
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
	b.mu.Unlock()

	for _, t := range topics {
		if err := b.wireTopic(cm, t, queue); err != nil {
			// Left unwired; retried on the next connection-up.
			logger.Error(err, "mqtt5 subscription wiring failed", "topic", t)
		}
	}
}

func (b *mqtt5MessageBus) unsubscribe(topic string, queue bool, sub *mqtt5Subscription) {
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
	ctx, cancel := context.WithTimeout(b.connCtx, mqtt5WireTimeout)
	defer cancel()
	_, _ = cm.Unsubscribe(ctx, &paho.Unsubscribe{
		Topics: []string{filter},
	})
}

func (b *mqtt5MessageBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subs []*mqtt5Subscription
	for _, l := range b.subs {
		subs = append(subs, l.all()...)
	}
	for _, l := range b.queues {
		subs = append(subs, l.all()...)
	}
	b.mu.Unlock()

	b.connCancel()
	for _, s := range subs {
		s.cancel()
	}
	ctx, cancel := context.WithTimeout(context.Background(), mqtt5WireTimeout)
	defer cancel()
	_ = b.sub.Disconnect(ctx)
	_ = b.queue.Disconnect(ctx)
	return nil
}

// ---- mqtt5SubList ----

type mqtt5SubList struct {
	mu   sync.Mutex
	subs []*mqtt5Subscription
	next int

	// wired is guarded by the bus mutex, not the list's own: it is read and
	// written by the wire path, which coordinates through the bus lock.
	wired bool
}

func (l *mqtt5SubList) add(sub *mqtt5Subscription) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs = append(l.subs, sub)
}

func (l *mqtt5SubList) remove(sub *mqtt5Subscription) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.subs, sub)
	if i == -1 {
		return false
	}
	l.subs = slices.Delete(l.subs, i, i+1)
	return true
}

func (l *mqtt5SubList) empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.subs) == 0
}

func (l *mqtt5SubList) all() []*mqtt5Subscription {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.subs)
}

func (l *mqtt5SubList) dispatch(payload []byte) {
	l.mu.Lock()
	subs := slices.Clone(l.subs)
	l.mu.Unlock()
	for _, sub := range subs {
		sub.write(payload)
	}
}

func (l *mqtt5SubList) dispatchQueue(payload []byte) {
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

// ---- mqtt5Subscription ----

type mqtt5Subscription struct {
	bus    *mqtt5MessageBus
	ctx    context.Context
	cancel context.CancelFunc
	topic  string
	queue  bool
	ch     chan []byte
}

func (s *mqtt5Subscription) write(payload []byte) {
	select {
	case s.ch <- payload:
	case <-s.ctx.Done():
	}
}

func (s *mqtt5Subscription) read() ([]byte, bool) {
	select {
	case payload, ok := <-s.ch:
		return payload, ok
	case <-s.ctx.Done():
		return nil, false
	}
}

func (s *mqtt5Subscription) Close() error {
	s.cancel()
	s.bus.unsubscribe(s.topic, s.queue, s)
	return nil
}
