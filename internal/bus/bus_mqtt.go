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
	// mqttReconcileInterval is how often the reconciler retries
	// subscriptions that are not established on the broker, whether they
	// were created during an outage or failed (re)establishment.
	mqttReconcileInterval = time.Second
	// mqttProbeTimeout bounds the shared-subscription probe at startup.
	mqttProbeTimeout = time.Second * 2
	// mqttPubPoolSize is the number of dedicated publish clients. paho
	// serializes all publishes of one client through its outbound loop, so
	// concurrent publishers are spread over several connections, mirroring
	// the connection split between broadcast and queue traffic.
	mqttPubPoolSize = 4
)

var errMqttClosed = errors.New("psrpc: mqtt message bus closed")

// mqttName maps a psrpc channel to an MQTT topic.
//
// Channel parts are sanitized by psrpc to [0-9A-Za-z_], with every other
// rune escaped to u+XXXX / U+XXXXXX, so the legacy string consists solely
// of [0-9A-Za-z_|+]: '|' as the delimiter and '+' inside escape sequences.
// Both are transformed, '|' to the MQTT level separator '/' and '+' to '-',
// which can never occur in a legacy string — so the mapping cannot collide.
// '+' and '#' must not survive because they are MQTT wildcards, and no level
// can start with '$' because parts always start with [0-9A-Za-z_].
func mqttName(channel Channel) string {
	return strings.NewReplacer("|", "/", "+", "-").Replace(channel.Legacy)
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
// It owns several broker connections:
//   - the broadcast connection carries plain subscriptions (every
//     subscriber gets a copy of each message);
//   - the queue connection joins a shared subscription ($share/psrpc/...)
//     per topic, giving competing-consumer semantics across processes;
//   - publish clients do nothing but publish.
//
// Two subscription connections are needed because a broker delivers a
// message once per matching subscription: a topic with both subscription
// kinds would arrive twice on a single connection and the delivery for the
// queue subscription could not be told apart from the broadcast one.
//
// paho reconnects internally with clean sessions, which drops all
// broker-side state; a reconciler re-establishes every subscription after
// each (re)connect and keeps retrying the ones that fail, so subscriptions
// created while the connection is down are accepted and simply start
// delivering once it is back — mirroring the Redis bus.
//
// Dispatch is ordered: paho's router runs handlers from a single goroutine
// in arrival order (SetOrderMatters defaults to true), which is the
// ordering guarantee psrpc streams rely on and the Redis, NATS and Local
// buses also provide.
//
// Still not production-hardened: no metrics, and messages published while
// the connection is down fail fast rather than being buffered.
type mqttMessageBus struct {
	sub   mqtt.Client   // broadcast subscriptions
	queue mqtt.Client   // shared subscriptions
	pubs  []mqtt.Client // dedicated publish clients

	ctx    context.Context
	cancel context.CancelFunc
	kick   chan struct{} // nudges the reconciler after a reconnect

	mu     sync.Mutex
	subs   map[string]*mqttSubList // broadcast subscribers, keyed by topic
	queues map[string]*mqttSubList // shared subscribers, keyed by topic
	closed bool

	c       *compressor
	maxSize int
}

// NewMqttMessageBus dials the broker and starts the bus. The factory is called
// once per connection (the bus maintains several) because paho's ClientOptions
// cannot be cloned and each connection needs a distinct client ID; it must
// return a fresh options value on every call.
//
// The broker must honor shared subscriptions ($share). A broker that merely
// accepts the filter as a literal topic would silently starve every queue
// RPC, so support is probed before the bus is returned.
func NewMqttMessageBus(newOptions func() *mqtt.ClientOptions, opts ...BusOption) (*mqttMessageBus, error) {
	o := getBusOpts(opts...)
	ctx, cancel := context.WithCancel(context.Background())
	b := &mqttMessageBus{
		subs:   map[string]*mqttSubList{},
		queues: map[string]*mqttSubList{},
		ctx:    ctx,
		cancel: cancel,
		kick:   make(chan struct{}, 1),
		c:      newCompressor(o.Compression),
		maxSize: o.Compression.MaxDecompressedSize,
	}
	connect := func(c mqtt.Client) error {
		if t := c.Connect(); !t.WaitTimeout(mqttConnectTimeout) || t.Error() != nil {
			err := t.Error()
			if err == nil {
				err = errors.New("timed out")
			}
			return fmt.Errorf("psrpc: mqtt connect failed: %w", err)
		}
		return nil
	}

	b.sub = b.newClient(newOptions, "-b", b.onSubConnect, nil)
	b.queue = b.newClient(newOptions, "-q", b.onQueueConnect, b.dispatchQueueMessage)
	b.pubs = nil
	for i := 0; i < mqttPubPoolSize; i++ {
		b.pubs = append(b.pubs, b.newClient(newOptions, fmt.Sprintf("-p%d", i), nil, nil))
	}

	clients := append([]mqtt.Client{b.sub, b.queue}, b.pubs...)
	for _, c := range clients {
		if err := connect(c); err != nil {
			// disconnect everything established so far: the clients
			// auto-reconnect in the background and would otherwise leak
			// connections per construction attempt
			b.shutdownClients(clients)
			cancel()
			return nil, err
		}
	}

	if err := b.probeSharedSubscriptions(); err != nil {
		b.shutdownClients(clients)
		cancel()
		return nil, err
	}

	go b.reconcileLoop()
	return b, nil
}

func (b *mqttMessageBus) shutdownClients(clients []mqtt.Client) {
	for _, c := range clients {
		c.Disconnect(0)
	}
}

func (b *mqttMessageBus) maxDecompressedSize() int {
	return b.maxSize
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
	// Order matters: paho's default router dispatches inbound messages from
	// a single goroutine in arrival order, which is the per-channel
	// ordering psrpc requires (Redis, NATS and Local dispatch the same way).
	// SetOrderMatters(false) would spawn one goroutine per message and let
	// them race into the subscription out of order.
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

// probeSharedSubscriptions verifies the broker actually delivers on a
// $share filter. Some brokers without shared-subscription support accept the
// SUBSCRIBE with return code 0 and treat the filter as a literal topic,
// which would leave every queue RPC silently unanswered.
func (b *mqttMessageBus) probeSharedSubscriptions() error {
	group := rand.NewString()
	topic := "psrpc-probe-" + rand.NewString()
	filter := "$share/" + group + "/" + topic

	got := make(chan struct{}, 1)
	t := b.queue.SubscribeMultiple(map[string]byte{filter: mqttQoS}, func(_ mqtt.Client, _ mqtt.Message) {
		select {
		case got <- struct{}{}:
		default:
		}
	})
	if err := checkSubscribeToken(t, filter); err != nil {
		return err
	}
	// paho registers the route for a $share filter under the stripped
	// topic, so both the filter and the bare topic are unsubscribed to
	// release the route along with the subscription.
	defer func() {
		_ = b.queue.Unsubscribe(filter)
		_ = b.queue.Unsubscribe(topic)
	}()

	pt := b.pubs[0].Publish(topic, mqttQoS, false, []byte{1})
	if !pt.WaitTimeout(mqttSubscribeTimeout) {
		return errors.New("psrpc: mqtt shared-subscription probe publish timed out")
	} else if err := pt.Error(); err != nil {
		return fmt.Errorf("psrpc: mqtt shared-subscription probe publish failed: %w", err)
	}

	select {
	case <-got:
		return nil
	case <-time.After(mqttProbeTimeout):
		return errors.New("psrpc: broker does not support MQTT shared subscriptions ($share), required by SubscribeQueue")
	}
}

// onSubConnect unwires every broadcast subscription after a (re)connect.
func (b *mqttMessageBus) onSubConnect(mqtt.Client) {
	b.reconnect(b.subs)
}

// onQueueConnect unwires every shared subscription after a (re)connect.
// Deliveries fall through to the queue client's default publish handler,
// which routes by original topic.
func (b *mqttMessageBus) onQueueConnect(mqtt.Client) {
	b.reconnect(b.queues)
}

// reconnect marks a client's subscriptions unwired and nudges the
// reconciler: clean sessions drop all broker-side state on (re)connect.
func (b *mqttMessageBus) reconnect(lists map[string]*mqttSubList) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	for _, l := range lists {
		l.setWired(false)
	}
	b.mu.Unlock()

	select {
	case b.kick <- struct{}{}:
	default:
	}
}

// reconcileLoop periodically establishes subscriptions that are not wired on
// the broker: created while a connection was down, unwired by a reconnect,
// or whose (re)establishment failed.
func (b *mqttMessageBus) reconcileLoop() {
	for {
		b.reconcilePass()
		select {
		case <-b.kick:
		case <-time.After(mqttReconcileInterval):
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *mqttMessageBus) reconcilePass() {
	type pending struct {
		client mqtt.Client
		topic  string
		list   *mqttSubList
		queue  bool
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	pendings := make([]pending, 0, len(b.subs)+len(b.queues))
	for t, l := range b.subs {
		if !l.isWired() {
			pendings = append(pendings, pending{b.sub, t, l, false})
		}
	}
	for t, l := range b.queues {
		if !l.isWired() {
			pendings = append(pendings, pending{b.queue, t, l, true})
		}
	}
	b.mu.Unlock()

	for _, p := range pendings {
		if !p.client.IsConnectionOpen() {
			return // next pass, or the kick after reconnect
		}
		if err := mqttWire(p.client, p.topic, p.list, p.queue); err != nil {
			// the list stays unwired and is retried; a broker that keeps
			// refusing (ACL misconfiguration, for example) stays visible
			// in the logs
			logger.Error(err, "mqtt subscribe failed", "topic", p.topic)
			continue
		}
		p.list.setWired(true)
		// The last subscriber may have closed while the SUBSCRIBE was on
		// the wire; undo it instead of leaving a dead subscription and
		// route behind.
		b.mu.Lock()
		lists := b.subs
		if p.queue {
			lists = b.queues
		}
		_, live := lists[p.topic]
		b.mu.Unlock()
		if !live {
			p.list.setWired(false)
			filter := p.topic
			if p.queue {
				filter = mqttQueueFilter(p.topic)
			}
			_ = p.client.Unsubscribe(filter)
		}
	}
}

func (b *mqttMessageBus) Publish(_ context.Context, channel Channel, msg proto.Message) error {
	payload, err := serialize(msg, "", b.c)
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

	if b.closed {
		b.mu.Unlock()
		cancel()
		return nil, errMqttClosed
	}

	lists, client := b.subs, b.sub
	if queue {
		lists, client = b.queues, b.queue
	}
	if list, ok := lists[topic]; ok {
		list.add(sub)
		b.mu.Unlock()
		return sub, nil
	}
	// The creator registers an unwired list first so concurrent subscribers
	// of the same topic find it, then establishes the broker subscription
	// outside b.mu: the round trip must not block publishes and queue
	// dispatch for up to the subscribe timeout.
	list := &mqttSubList{}
	lists[topic] = list
	b.mu.Unlock()

	if client.IsConnectionOpen() {
		if err := mqttWire(client, topic, list, queue); err != nil {
			if client.IsConnectionOpen() {
				// a broker refusal or timeout, not a lost connection
				b.mu.Lock()
				if cur, ok := lists[topic]; ok && cur == list {
					// keep the list when another subscriber joined
					// meanwhile; the reconciler keeps retrying the wiring
					// for them
					if list.empty() {
						delete(lists, topic)
					}
				}
				b.mu.Unlock()
				cancel()
				return nil, err
			}
			// the connection dropped mid-subscribe: the reconciler wires
			// the list after reconnect
		} else {
			list.setWired(true)
		}
	}
	// While the connection is down the list stays unwired; the reconciler
	// establishes it after the next (re)connect.

	list.add(sub)
	return sub, nil
}

// mqttWire establishes the broker-side subscription. Both a transport
// failure and a SUBACK refusal code are returned as errors: paho reports
// neither on the token for a broker that answers 0x80.
func mqttWire(c mqtt.Client, topic string, list *mqttSubList, queue bool) error {
	filter := topic
	var t mqtt.Token
	if queue {
		filter = mqttQueueFilter(topic)
		// nil callback for shared subscriptions: paho stores routes for
		// $share filters under the stripped topic where Unsubscribe cannot
		// reach them, so deliveries fall through to the queue client's
		// default publish handler, which routes by original topic.
		t = c.SubscribeMultiple(map[string]byte{filter: mqttQoS}, nil)
	} else {
		t = c.Subscribe(topic, mqttQoS, mqttDispatchHandler(list))
	}
	return checkSubscribeToken(t, filter)
}

func checkSubscribeToken(t mqtt.Token, filter string) error {
	if !t.WaitTimeout(mqttSubscribeTimeout) {
		return fmt.Errorf("psrpc: mqtt subscribe to %q timed out", filter)
	}
	if err := t.Error(); err != nil {
		return fmt.Errorf("psrpc: mqtt subscribe to %q failed: %w", filter, err)
	}
	if st, ok := t.(*mqtt.SubscribeToken); ok {
		for _, rc := range st.Result() {
			if rc >= 0x80 {
				return fmt.Errorf("psrpc: mqtt subscribe to %q refused by broker (code %#x)", filter, rc)
			}
		}
	}
	return nil
}

func (b *mqttMessageBus) unsubscribe(topic string, queue bool, sub *mqttSubscription) {
	b.mu.Lock()

	lists, client := b.subs, b.sub
	filter := topic
	if queue {
		lists, client = b.queues, b.queue
		filter = mqttQueueFilter(topic)
	}
	list, ok := lists[topic]
	if !ok {
		b.mu.Unlock()
		return
	}
	if !list.remove(sub) {
		b.mu.Unlock()
		return
	}
	if list.empty() {
		delete(lists, topic)
		b.mu.Unlock()
		// Best effort: if the client is down the broker state is gone with
		// the clean session anyway, and the reconciler only re-establishes
		// lists still present in the map.
		_ = client.Unsubscribe(filter)
		return
	}
	b.mu.Unlock()
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

	b.cancel()
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
	// wired records whether the broker-side subscription exists. It is
	// guarded by mu like the rest, but read under the bus lock as well
	// (lock order is always bus.mu before list.mu).
	wired bool
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

func (l *mqttSubList) isWired() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.wired
}

func (l *mqttSubList) setWired(wired bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.wired = wired
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
