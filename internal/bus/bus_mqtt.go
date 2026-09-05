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
	"slices"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/rand"
)

const (
	// mqttQueueGroup is the shared-subscription group name. Every process
	// using psrpc joins the same group, so the broker round-robins each
	// queue message to exactly one process.
	mqttQueueGroup = "psrpc"
	mqttQoS        = byte(0)
	// QoS 0 keeps the at-most-once semantics psrpc already tolerates
	// (request timeouts and claims absorb lost messages), matching the
	// Redis and AMQP buses which also don't acknowledge deliveries.
	mqttConnectTimeout   = time.Second * 10
	mqttSubscribeTimeout = time.Second * 10
	// paho defaults to 10 minutes between reconnection attempts; psrpc
	// request latencies can't absorb that, so it is clamped.
	mqttMaxReconnectInterval = time.Second * 5
	// mqttPubPoolSize is the number of dedicated publish clients. paho
	// serializes all publishes of one client through its outbound loop, so
	// concurrent publishers are spread over several connections, mirroring
	// the connection split between broadcast and queue traffic.
	mqttPubPoolSize = 4
)

var errMqttClosed = errors.New("psrpc: mqtt message bus closed")

// mqttName maps a psrpc channel to an MQTT topic. Channel parts are sanitized
// by psrpc to [0-9A-Za-z_], so '|' only ever appears as the legacy delimiter,
// and MQTT's reserved characters ('+', '#', NUL) can never occur. Replacing
// '|' with '/' keeps topics hierarchical and cannot collide.
func mqttName(channel Channel) string {
	return strings.ReplaceAll(channel.Legacy, "|", "/")
}

// mqttPubSlotIndex pins all publishes to one topic on the same paho
// client: ordering is per connection only, and psrpc relies on
// per-channel ordering (a stream ack must not overtake its close).
func mqttPubSlotIndex(topic string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(topic))
	return int(h.Sum32() % uint32(mqttPubPoolSize))
}

func mqttQueueFilter(topic string) string {
	return "$share/" + mqttQueueGroup + "/" + topic
}

// mqttMessageBus is an MQTT (3.1.1) MessageBus with automatic reconnection.
//
// It owns two broker connections:
//   - the broadcast connection carries all publishes and plain subscriptions
//     (every subscriber gets a copy of each message);
//   - the queue connection joins a shared subscription ($share/psrpc/...)
//     per topic, giving competing-consumer semantics across processes.
//
// Two connections are needed because a broker delivers a message once per
// matching subscription: a topic with both subscription kinds would arrive
// twice on a single connection and the delivery for the queue subscription
// could not be told apart from the broadcast one.
//
// paho handles reconnection internally; because clean sessions drop broker
// state, every active subscription is re-established from the OnConnect
// callback after each (re)connect.
//
// Still not production-hardened: no metrics, and messages published while the
// connection is down fail fast rather than being buffered.
type mqttMessageBus struct {
	sub   mqtt.Client   // broadcast subscriptions
	queue mqtt.Client   // shared subscriptions
	pubs  []mqtt.Client // dedicated publish clients, picked round-robin

	mu     sync.Mutex
	subs   map[string]*mqttSubList // broadcast subscribers, keyed by topic
	queues map[string]*mqttSubList // shared subscribers, keyed by topic
	closed bool
}

// NewMqttMessageBus dials the broker and starts the bus. The factory is called
// once per connection (the bus maintains two) because paho's ClientOptions
// cannot be cloned and each connection needs a distinct client ID; it must
// return a fresh options value on every call.
func NewMqttMessageBus(newOptions func() *mqtt.ClientOptions) (*mqttMessageBus, error) {
	b := &mqttMessageBus{
		subs:   map[string]*mqttSubList{},
		queues: map[string]*mqttSubList{},
	}
	connect := func(c mqtt.Client) error {
		if t := c.Connect(); !t.WaitTimeout(mqttConnectTimeout) || t.Error() != nil {
			err := t.Error()
			if err == nil {
				err = errors.New("timed out")
			}
			c.Disconnect(0)
			return fmt.Errorf("psrpc: mqtt connect failed: %w", err)
		}
		return nil
	}
	b.sub = b.newClient(newOptions, "-b", b.onSubConnect, nil)
	b.queue = b.newClient(newOptions, "-q", b.onQueueConnect, b.dispatchQueueMessage)
	b.pubs = b.pubClients(newOptions)
	for _, c := range append([]mqtt.Client{b.sub, b.queue}, b.pubs...) {
		if err := connect(c); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// pubClients dials the dedicated publish clients. They subscribe to nothing,
// so no OnConnect resubscription applies; paho reconnects them on its own.
func (b *mqttMessageBus) pubClients(newOptions func() *mqtt.ClientOptions) []mqtt.Client {
	for i := 0; i < mqttPubPoolSize; i++ {
		b.pubs = append(b.pubs, b.newClient(newOptions, fmt.Sprintf("-p%d", i), nil, nil))
	}
	return b.pubs
}

func (b *mqttMessageBus) newClient(
	newOptions func() *mqtt.ClientOptions,
	suffix string,
	onConnect mqtt.OnConnectHandler,
	defaultHandler mqtt.MessageHandler,
) mqtt.Client {
	o := newOptions()
	if o.ClientID == "" {
		o.ClientID = "psrpc-" + rand.NewString()
	}
	o.ClientID += suffix
	o.SetCleanSession(true)
	o.SetAutoReconnect(true)
	o.SetOrderMatters(false)
	if o.MaxReconnectInterval >= mqttMaxReconnectInterval {
		o.SetMaxReconnectInterval(mqttMaxReconnectInterval)
	}
	if o.ConnectTimeout == 0 {
		o.SetConnectTimeout(mqttConnectTimeout)
	}
	o.SetOnConnectHandler(onConnect)
	o.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		logger.Error(err, "mqtt connection lost")
	})
	if defaultHandler != nil {
		o.SetDefaultPublishHandler(defaultHandler)
	}
	return mqtt.NewClient(o)
}

// onSubConnect re-establishes every broadcast subscription after a (re)connect.
func (b *mqttMessageBus) onSubConnect(c mqtt.Client) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	type entry struct {
		topic string
		list  *mqttSubList
	}
	entries := make([]entry, 0, len(b.subs))
	for t, l := range b.subs {
		entries = append(entries, entry{t, l})
	}
	b.mu.Unlock()

	for _, e := range entries {
		t := c.Subscribe(e.topic, mqttQoS, mqttDispatchHandler(e.list))
		if !t.WaitTimeout(mqttSubscribeTimeout) || t.Error() != nil {
			logger.Error(t.Error(), "mqtt resubscribe failed", "topic", e.topic)
			continue
		}
		// The subscription may have been closed while it was being
		// re-established; undo the broker-side subscription if so.
		b.mu.Lock()
		gone := b.closed || b.subs[e.topic] != e.list
		b.mu.Unlock()
		if gone {
			_ = c.Unsubscribe(e.topic)
		}
	}
}

// onQueueConnect re-establishes every shared subscription after a (re)connect.
// Callbacks are nil so paho doesn't register routes; deliveries fall through
// to the default publish handler, which routes by original topic.
func (b *mqttMessageBus) onQueueConnect(c mqtt.Client) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	type entry struct {
		topic  string
		filter string
		list   *mqttSubList
	}
	entries := make([]entry, 0, len(b.queues))
	for t, l := range b.queues {
		entries = append(entries, entry{t, mqttQueueFilter(t), l})
	}
	b.mu.Unlock()

	for _, e := range entries {
		t := c.SubscribeMultiple(map[string]byte{e.filter: mqttQoS}, nil)
		if !t.WaitTimeout(mqttSubscribeTimeout) || t.Error() != nil {
			logger.Error(t.Error(), "mqtt resubscribe failed", "topic", e.topic)
			continue
		}
		b.mu.Lock()
		gone := b.closed || b.queues[e.topic] != e.list
		b.mu.Unlock()
		if gone {
			_ = c.Unsubscribe(e.filter)
		}
	}
}

func (b *mqttMessageBus) Publish(_ context.Context, channel Channel, msg proto.Message) error {
	payload, err := serialize(msg, "")
	if err != nil {
		return err
	}

	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return errMqttClosed
	}
	// Fail fast while the connection is down so request publishers can
	// retry; paho would otherwise queue the message into a dead socket.
	c := b.pubs[mqttPubSlotIndex(mqttName(channel))]
	if !c.IsConnectionOpen() {
		return errors.New("psrpc: mqtt connection is down")
	}
	return c.Publish(mqttName(channel), mqttQoS, false, payload).Error()
}

func (b *mqttMessageBus) Subscribe(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, mqttName(channel), false, size)
}

func (b *mqttMessageBus) SubscribeQueue(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, mqttName(channel), true, size)
}

func (b *mqttMessageBus) subscribe(ctx context.Context, topic string, queue bool, size int) (Reader, error) {
	subCtx, cancel := context.WithCancel(ctx)
	sub := &mqttSubscription{
		bus:    b,
		ctx:    subCtx,
		cancel: cancel,
		topic:  topic,
		queue:  queue,
		ch:     make(chan []byte, size),
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		cancel()
		return nil, errMqttClosed
	}

	lists := b.subs
	client := b.sub
	filter := topic
	wireSubscribe := func(list *mqttSubList) mqtt.Token {
		return client.Subscribe(topic, mqttQoS, mqttDispatchHandler(list))
	}
	if queue {
		lists = b.queues
		client = b.queue
		filter = mqttQueueFilter(topic)
		wireSubscribe = func(list *mqttSubList) mqtt.Token {
			// nil callback: no route is registered and deliveries fall
			// through to the default publish handler. Routes registered
			// for a $share filter can't be removed again, because
			// paho stores them under the stripped topic while
			// Unsubscribe only deletes the full filter string.
			return client.SubscribeMultiple(map[string]byte{filter: mqttQoS}, nil)
		}
	}

	list, ok := lists[topic]
	if !ok {
		list = &mqttSubList{}
		t := wireSubscribe(list)
		if !t.WaitTimeout(mqttSubscribeTimeout) || t.Error() != nil {
			err := t.Error()
			if err == nil {
				err = errors.New("timed out")
			}
			cancel()
			return nil, fmt.Errorf("psrpc: mqtt subscribe to %q failed: %w", filter, err)
		}
		lists[topic] = list
	}
	list.add(sub)
	return sub, nil
}

func (b *mqttMessageBus) unsubscribe(topic string, queue bool, sub *mqttSubscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	lists := b.subs
	filter := topic
	client := b.sub
	if queue {
		lists = b.queues
		filter = mqttQueueFilter(topic)
		client = b.queue
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
		// Best effort: if this runs during a disconnect the broker
		// state is gone anyway and OnConnect works off the maps.
		_ = client.Unsubscribe(filter)
	}
}

// dispatchQueueMessage is the queue client's default publish handler; shared
// subscription deliveries carry the original topic name.
func (b *mqttMessageBus) dispatchQueueMessage(_ mqtt.Client, m mqtt.Message) {
	b.mu.Lock()
	list := b.queues[m.Topic()]
	b.mu.Unlock()
	if list != nil {
		list.dispatchQueue(m.Payload())
	}
}

func (b *mqttMessageBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	var subs []*mqttSubscription
	for _, l := range b.subs {
		subs = append(subs, l.all()...)
	}
	for _, l := range b.queues {
		subs = append(subs, l.all()...)
	}
	b.mu.Unlock()

	for _, s := range subs {
		s.cancel()
	}
	b.sub.Disconnect(250)
	b.queue.Disconnect(250)
	for _, c := range b.pubs {
		c.Disconnect(250)
	}
	return nil
}

// ----------------------------------------------

type mqttSubList struct {
	mu   sync.Mutex
	subs []*mqttSubscription
	next int
}

func (l *mqttSubList) add(sub *mqttSubscription) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.subs = append(l.subs, sub)
}

func (l *mqttSubList) remove(sub *mqttSubscription) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := slices.Index(l.subs, sub)
	if i == -1 {
		return false
	}
	l.subs = slices.Delete(l.subs, i, i+1)
	return true
}

func (l *mqttSubList) empty() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.subs) == 0
}

func (l *mqttSubList) all() []*mqttSubscription {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.subs)
}

// dispatch delivers the payload to every subscriber. The subscriber list is
// snapshotted so a slow subscriber can't block subscription bookkeeping;
// a closed subscriber's write returns immediately through its canceled
// context instead of a closed channel, so stale snapshots are safe.
func (l *mqttSubList) dispatch(payload []byte) {
	l.mu.Lock()
	subs := slices.Clone(l.subs)
	l.mu.Unlock()
	for _, sub := range subs {
		sub.write(payload)
	}
}

// dispatchQueue delivers the payload to exactly one of the local subscribers;
// combined with the shared subscription this yields competing consumers.
func (l *mqttSubList) dispatchQueue(payload []byte) {
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

func mqttDispatchHandler(list *mqttSubList) mqtt.MessageHandler {
	return func(_ mqtt.Client, m mqtt.Message) {
		list.dispatch(m.Payload())
	}
}

// ----------------------------------------------

type mqttSubscription struct {
	bus    *mqttMessageBus
	ctx    context.Context
	cancel context.CancelFunc
	topic  string
	queue  bool
	ch     chan []byte
}

func (s *mqttSubscription) write(payload []byte) {
	select {
	case s.ch <- payload:
	case <-s.ctx.Done():
	}
}

func (s *mqttSubscription) read() ([]byte, bool) {
	select {
	case payload, ok := <-s.ch:
		return payload, ok
	case <-s.ctx.Done():
		// The channel is intentionally never closed: in-flight writers
		// exit through the context instead of racing with close.
		return nil, false
	}
}

func (s *mqttSubscription) Close() error {
	s.cancel()
	s.bus.unsubscribe(s.topic, s.queue, s)
	return nil
}
