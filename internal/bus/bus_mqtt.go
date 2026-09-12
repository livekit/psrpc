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
	"hash/fnv"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/eclipse/paho.golang/autopaho"
	"github.com/eclipse/paho.golang/paho"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/pkg/rand"
)

const (
	mqtt5QueueGroup        = "psrpc"
	mqtt5QoS               = byte(0)
	mqtt5ConnectTimeout    = 10 * time.Second
	mqtt5SessionExpiry     = 3600 // seconds: broker retains subs for 1h across disconnects
	mqtt5KeepAlive         = 10 * time.Second
	mqtt5PubPoolSize       = 4
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

func mqtt5PubSlotIndex(topic string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(topic))
	return int(h.Sum32() % uint32(mqtt5PubPoolSize))
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
// on reconnect: the broker retains subscriptions across disconnects. Only
// local OnPublishReceived handlers need re-registration (they are in-process,
// not broker state), and OnConnectionUp handles that with a simple iteration
// of the active topic lists.
//
// Ordered dispatch is guaranteed: paho.golang drains publishPackets from a
// single goroutine in arrival order (client.go:196-257), matching the
// Redis/NATS/Local dispatch model.
type mqtt5MessageBus struct {
	sub   *autopaho.ConnectionManager // broadcast
	queue *autopaho.ConnectionManager // shared subscriptions

	connCtx    context.Context
	connCancel context.CancelFunc

	mu     sync.Mutex
	subs   map[string]*mqtt5SubList // broadcast subscribers
	queues map[string]*mqtt5SubList // shared subscribers
	closed bool

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

	start := func(suffix string, onPub func(autopaho.PublishReceived) (bool, error)) (*autopaho.ConnectionManager, error) {
		subCfg := autopaho.ClientConfig{
			BrokerUrls:                    []*url.URL{parsed},
			KeepAlive:                     uint16(mqtt5KeepAlive.Seconds()),
			ConnectTimeout:                mqtt5ConnectTimeout,
			CleanStartOnInitialConnection: true,
			SessionExpiryInterval:         mqtt5SessionExpiry,
			ClientConfig: paho.ClientConfig{
				ClientID: clientID + suffix,
			},
			OnConnectionUp: func(cm *autopaho.ConnectionManager, _ *paho.Connack) {
				b.reconnectHandlers(cm == b.queue)
			},
		}
if onPub != nil {
				subCfg.ClientConfig.OnPublishReceived = []func(paho.PublishReceived) (bool, error){
					func(pr paho.PublishReceived) (bool, error) {
						return onPub(autopaho.PublishReceived{PublishReceived: pr, ConnectionManager: nil})
					},
				}
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

	b.sub, err = start("-b", nil)
	if err != nil {
		cancel()
		return nil, err
	}
b.queue, err = start("-q", nil)
	if err != nil {
		_ = b.sub.Disconnect(ctx)
		cancel()
		return nil, err
	}

	return b, nil
}

// reconnectHandlers re-registers OnPublishReceived callbacks on the new
// client after a reconnect. Broker subscriptions persist thanks to
// SessionExpiryInterval; only the in-process callback routing needs this.
func (b *mqtt5MessageBus) reconnectHandlers(queue bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	cm := b.sub
	lists := b.subs
	if queue {
		cm = b.queue
		lists = b.queues
	}
	for topic, list := range lists {
		list.mu.Lock()
		empty := len(list.subs) == 0
		list.mu.Unlock()
		if empty {
			continue
		}
t := topic
			q := queue
			_ = cm.AddOnPublishReceived(func(pr autopaho.PublishReceived) (bool, error) {
				if pr.Packet.Topic == t {
					if q {
						list.dispatchQueue(pr.Packet.Payload)
					} else {
						list.dispatch(pr.Packet.Payload)
					}
				}
				return true, nil
			})
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

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return nil, errMqtt5Closed
	}

	lists, cm := b.subs, b.sub
	filter := topic
	wireSubscribe := func() error {
		_, err := cm.Subscribe(context.Background(), &paho.Subscribe{
			Subscriptions: []paho.SubscribeOptions{
				{Topic: filter, QoS: mqtt5QoS},
			},
		})
		return err
	}
	if queue {
		lists, cm = b.queues, b.queue
		filter = mqtt5QueueFilter(topic)
		wireSubscribe = func() error {
			_, err := cm.Subscribe(context.Background(), &paho.Subscribe{
				Subscriptions: []paho.SubscribeOptions{
					{Topic: filter, QoS: mqtt5QoS},
				},
			})
			return err
		}
	}

list, ok := lists[topic]
	if !ok {
		list = &mqtt5SubList{}
		if err := wireSubscribe(); err != nil {
			b.mu.Unlock()
			cancel()
			return nil, fmt.Errorf("psrpc: mqtt5 subscribe to %q failed: %w", filter, err)
		}
		lists[topic] = list
		// Register a topic-scoped handler on the first subscriber.
		// Subsequent subscribers share it. reconnectHandlers
		// re-registers on reconnect thanks to SessionExpiryInterval.
		t := topic
		l := list
		q := queue
		_ = cm.AddOnPublishReceived(func(pr autopaho.PublishReceived) (bool, error) {
			if pr.Packet.Topic == t {
				if q {
					l.dispatchQueue(pr.Packet.Payload)
				} else {
					l.dispatch(pr.Packet.Payload)
				}
			}
			return true, nil
		})
	}
	list.add(sub)
	b.mu.Unlock()
	return sub, nil
}

func (b *mqtt5MessageBus) unsubscribe(topic string, queue bool, sub *mqtt5Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	lists, cm := b.subs, b.sub
	filter := topic
	if queue {
		lists, cm = b.queues, b.queue
		filter = mqtt5QueueFilter(topic)
	}
	list, ok := lists[topic]
	if !ok {
		return
	}
	if !list.remove(sub) {
		return
	}
	if list.empty() {
		delete(lists, topic)
		// Best-effort unsubscribe. SessionExpiryInterval means the broker
		// cleans up on its own eventually.
		_, _ = cm.Unsubscribe(context.Background(), &paho.Unsubscribe{
			Topics: []string{filter},
		})
	}
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
	_ = b.sub.Disconnect(context.Background())
	_ = b.queue.Disconnect(context.Background())
	return nil
}

// ---- mqtt5SubList ----

type mqtt5SubList struct {
	mu   sync.Mutex
	subs []*mqtt5Subscription
	next int
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
	if l.next >= len(l.subs) {
		l.next = 0
	}
	if len(l.subs) == 0 {
		l.mu.Unlock()
		return
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