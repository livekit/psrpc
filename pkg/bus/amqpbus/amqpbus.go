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

// Package amqpbus provides a psrpc bus backed by AMQP 0-9-1 (RabbitMQ).
package amqpbus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"

	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/rand"
)

// amqpMaxNameLen is the shortstr limit RabbitMQ enforces on exchange and
// queue names; psrpc escapes each non-ASCII rune in a topic to 6 bytes, so
// long room or identity names can exceed it.
const amqpMaxNameLen = 255

// amqpDialTimeout bounds the TCP connect and AMQP handshake so that a
// black-holed broker fails a publish within the caller's request deadline
// instead of amqp.Dial's 30s default.
const amqpDialTimeout = 3 * time.Second

// subChannelPoolSize is the number of channels shared by all subscriptions.
// RabbitMQ caps a connection at channel_max (2047 by default) and
// livekit-server opens ~14 subscriptions per participant, so one channel per
// subscription would exhaust the id space at ~147 participants per node.
// Consumers multiplex fine on a shared channel; the pool keeps that headroom.
const subChannelPoolSize = 16

// amqpDial is amqp.Dial bounded by amqpDialTimeout. amqp091-go has no
// context-aware dial and only takes the timeout from a connection_timeout URL
// parameter, so the call is raced against a timer instead; a connection that
// completes after the timer won is closed, not leaked.
func amqpDial(url string) (*amqp.Connection, error) {
	type dialResult struct {
		conn *amqp.Connection
		err  error
	}
	res := make(chan dialResult, 1)
	go func() {
		conn, err := amqp.Dial(url)
		res <- dialResult{conn, err}
	}()
	select {
	case r := <-res:
		return r.conn, r.err
	case <-time.After(amqpDialTimeout):
		go func() {
			if r := <-res; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, errors.New("psrpc: amqp dial timed out")
	}
}

// exchangeName maps a psrpc Server subject to an AMQP exchange/queue name.
// Server subjects use '.' as their own separator, which AMQP names allow
// (though not as the topic separator it would be in MQTT-like brokers).
// Names beyond the 255-byte shortstr limit are replaced by a deterministic
// hash of the mapped name: publishers and subscribers derive the same alias,
// and the 'psrpc:' prefix cannot collide with a mapped name because the
// subject alphabet never contains ':'.
func exchangeName(subject string) string {
	name := subject
	if len(name) <= amqpMaxNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	return "psrpc:" + hex.EncodeToString(sum[:16])
}

// pubSlot is one slot of the publisher pool, owning a dedicated broker
// connection. Publishing and delivering contend over connection-level flow
// control in RabbitMQ, so publish traffic is kept on its own connections,
// separate from the subscription connection. Slots dial lazily and
// self-heal after a connection loss on their next publish.
//
// Slots are selected by hashing the exchange name: AMQP preserves ordering
// per connection only, and psrpc relies on per-channel ordering (e.g. a
// stream's ack must not overtake its close), so all publishes to one channel
// must share a connection. Parallelism comes from different channels landing
// on different connections.
type pubSlot struct {
	// pubMu serializes publishes on this slot. It is NOT held while dialing:
	// publishers queue behind a redial for at most amqpDialTimeout, and the
	// other slots are unaffected.
	pubMu sync.Mutex

	mu       sync.Mutex // guards ch and declared
	conn     *amqp.Connection
	ch       *amqp.Channel
	declared map[string]struct{} // exchanges declared on this channel
}

// acquire returns a healthy channel, dialing if needed. The state lock is
// dropped for the dial so unrelated readers of the slot don't block on it.
func (p *pubSlot) acquire(url string) (*amqp.Channel, error) {
	p.mu.Lock()
	if p.ch != nil && !p.ch.IsClosed() {
		ch := p.ch
		p.mu.Unlock()
		return ch, nil
	}
	p.mu.Unlock()

	if p.conn == nil || p.conn.IsClosed() {
		conn, err := amqpDial(url)
		if err != nil {
			return nil, err
		}
		p.conn = conn
	}
	ch, err := p.conn.Channel()
	if err != nil {
		if p.conn.IsClosed() {
			p.conn = nil
		}
		return nil, err
	}
	p.mu.Lock()
	p.ch = ch
	// A fresh channel invalidates cached declarations: the previous channel
	// may have died mid-declare, and re-declaring is always safe.
	p.declared = make(map[string]struct{})
	p.mu.Unlock()
	return ch, nil
}

func (p *pubSlot) drop() {
	p.mu.Lock()
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
	p.mu.Unlock()
}

func (p *pubSlot) close() {
	p.drop()
	if p.conn != nil {
		_ = p.conn.Close()
		p.conn = nil
	}
}

const pubPoolSize = 4

func pubSlotIndex(name string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return h.Sum32() % pubPoolSize
}

// transport is an AMQP (RabbitMQ) bus.Transport with automatic reconnection.
// It owns one connection for subscriptions and a pool of connections for
// publishing. After a connection loss the transport redials with backoff;
// each subscription re-establishes itself (exchange, queue, binding, consumer)
// on the new connection from its own read loop, so a subscription is never
// closed behind the caller's back and a subscription created while the broker
// is down simply starts delivering once it is back — mirroring the Redis bus,
// which accepts subscriptions while disconnected and reconciles them later.
// Publishes during an outage fail fast with the AMQP error; psrpc's request
// timeouts absorb that.
//
// Still not production-hardened: no publisher confirms, no metrics.
type transport struct {
	url string

	ctx    context.Context
	cancel context.CancelFunc
	done   <-chan struct{}

	mu     sync.Mutex
	conn   *amqp.Connection          // carries all subscriptions
	subChs [subChannelPoolSize]*amqp.Channel // shared consumer channels
	subs   map[*subscription]struct{}
	routers map[string]*router // Local demux on one Server subject
	pubPool [pubPoolSize]pubSlot // dedicated publish connections
	closed  bool
}

// New dials the broker and starts connection supervision. TLS (amqps://) and
// URL query params like heartbeat are handled by amqp.Dial. The returned bus
// must be closed by the caller.
func New(url string, opts ...bus.BusOption) (*Bus, error) {
	conn, err := amqpDial(url)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &transport{
		url:     url,
		ctx:     ctx,
		cancel:  cancel,
		done:    ctx.Done(),
		conn:    conn,
		subs:    map[*subscription]struct{}{},
		routers: map[string]*router{},
	}
	go b.supervise()
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

// Close stops supervision, closes all subscriptions and the connections.
func (b *transport) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := make([]*subscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	for _, r := range b.routers {
		subs = append(subs, r.all()...)
	}
	conn := b.conn
	b.mu.Unlock()

	b.cancel()
	for _, s := range subs {
		_ = s.Close()
	}
	for i := range b.pubPool {
		b.pubPool[i].close()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (b *transport) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *transport) connectionDown() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn == nil || b.conn.IsClosed()
}

// ---- bus.Transport interface ----

func (b *transport) Publish(ctx context.Context, channel bus.Channel, payload []byte) error {
	if b.isClosed() {
		// Without this, the pub slots would happily redial a connection
		// nothing would ever close.
		return amqp.ErrClosed
	}
	name := exchangeName(channel.Server)

	slot := &b.pubPool[pubSlotIndex(name)]
	slot.pubMu.Lock()
	defer slot.pubMu.Unlock()

	ch, err := slot.acquire(b.url)
	if err != nil {
		return err
	}
	slot.mu.Lock()
	_, declared := slot.declared[name]
	slot.mu.Unlock()
	if !declared {
		// Exchanges are declared durable=false, autoDelete=false: the
		// synchronous declare guards against publishing after a broker
		// restart, and the broker reclaims everything on its next restart.
		// autoDelete exchanges are not used because a re-declare after the
		// last queue unbound leaves an exchange that never auto-deletes
		// again, and the broker's asynchronous channel close on a vanished
		// exchange silently discards frames already written on the channel.
		if err = ch.ExchangeDeclare(name, "fanout", false, false, false, false, nil); err != nil {
			slot.drop()
			return err
		}
		slot.mu.Lock()
		slot.declared[name] = struct{}{}
		slot.mu.Unlock()
	}
	if err = ch.PublishWithContext(ctx, name, "", false, false, amqp.Publishing{Body: payload}); err != nil {
		// Only tear the channel down when it is actually broken: a context
		// cancelled before the write must not kill a healthy channel and
		// force every publisher on this slot through a re-declare.
		if ch.IsClosed() {
			slot.drop()
		}
	}
	return err
}

func (b *transport) Subscribe(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return b.subscribe(ctx, channel.Server, size, false)
	}
	return b.subscribeRouter(ctx, channel, size, false)
}

func (b *transport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	if channel.Local == "" {
		return b.subscribe(ctx, channel.Server, size, true)
	}
	return b.subscribeRouter(ctx, channel, size, true)
}

// subChannel returns a pooled consumer channel. Subscriptions are assigned a
// stable channel by name hash so a topic's declares and consumes stay on one
// channel; channels are shared because RabbitMQ would otherwise cap the
// process at channel_max subscriptions.
func (b *transport) subChannel(name string) (*amqp.Channel, error) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	idx := int(h.Sum32() % subChannelPoolSize)

	b.mu.Lock()
	closed, conn, ch := b.closed, b.conn, b.subChs[idx]
	b.mu.Unlock()
	if closed {
		return nil, amqp.ErrClosed
	}
	if ch != nil && !ch.IsClosed() && conn != nil && !conn.IsClosed() {
		return ch, nil
	}
	if conn == nil || conn.IsClosed() {
		return nil, amqp.ErrClosed
	}
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	if cur := b.subChs[idx]; cur != nil && !cur.IsClosed() {
		// another goroutine won the race; keep theirs
		b.mu.Unlock()
		_ = ch.Close()
		return cur, nil
	}
	b.subChs[idx] = ch
	b.mu.Unlock()
	return ch, nil
}

func (b *transport) subscribe(ctx context.Context, subject string, size int, queue bool) (bus.Reader, error) {
	s, err := newSubscription(ctx, b, subject, size, queue)
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		_ = s.Close()
		return nil, amqp.ErrClosed
	}
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s, nil
}

func (b *transport) removeSub(s *subscription) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// supervise re-establishes the connection whenever it drops.
func (b *transport) supervise() {
	for {
		b.mu.Lock()
		conn, closed := b.conn, b.closed
		b.mu.Unlock()
		if closed || conn == nil {
			return
		}
		closeCh := conn.NotifyClose(make(chan *amqp.Error, 1))
		select {
		case <-closeCh:
			if b.isClosed() {
				return
			}
			if !b.recover() {
				return
			}
		case <-b.done:
			return
		}
	}
}

// recover redials with backoff and installs the new connection. Subscriptions
// notice through their delivery sources and re-establish themselves; this
// deliberately does not touch them, so a subscription is never closed or
// parked by a recovery pass that raced its own state.
func (b *transport) recover() bool {
	backoff := time.Millisecond * 100
	for {
		if b.isClosed() {
			return false
		}

		conn, err := amqpDial(b.url)
		if err == nil {
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				_ = conn.Close()
				return false
			}
			b.conn = conn
			// the old channels died with the connection; the pool refills lazily
			for i := range b.subChs {
				b.subChs[i] = nil
			}
			b.mu.Unlock()
			return true
		}

		select {
		case <-time.After(backoff):
			if backoff < 5*time.Second {
				backoff *= 2
			}
		case <-b.done:
			return false
		}
	}
}

type subscription struct {
	bus   *transport
	name  string // mapped exchange name
	queue bool   // shared competing-consumer queue instead of exclusive

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	ch         *amqp.Channel // pooled channel the consumer lives on
	tag        string        // consumer tag, stable across reconnects
	deliveries <-chan amqp.Delivery

	closed atomic.Bool
	msgs   chan []byte
	done   chan struct{}
	once   sync.Once

	// Set when the subscription is routed through a router (Local demux):
	// Close unwinds the router entry instead of the plain subscriber set.
	r     *router
	local string

	// sink, when set, receives every delivery in place of msgs. The router's
	// underlying broker subscription uses it to demultiplex by Local channel
	// instead of handing the payload to one reader.
	sink func(payload []byte)
}

func newSubscription(ctx context.Context, b *transport, subject string, size int, queue bool) (*subscription, error) {
	name := exchangeName(subject)
	subCtx, cancel := context.WithCancel(ctx)
	s := &subscription{
		bus:    b,
		name:   name,
		queue:  queue,
		ctx:    subCtx,
		cancel: cancel,
		// consumer tags are unique per channel and the channel is shared, so
		// two subscriptions to the same topic must not share a tag
		tag:  "psrpc-" + rand.NewString(),
		msgs: make(chan []byte, size),
		done: make(chan struct{}),
	}
	if err := s.reestablish(); err != nil {
		// While the broker is down the subscription is still accepted: the
		// pump keeps retrying and deliveries start once the bus redialed.
		// Any other failure (ACL, precondition) is surfaced to the caller.
		if !errors.Is(err, amqp.ErrClosed) || !b.connectionDown() {
			cancel()
			return nil, err
		}
	}
	go s.pump()
	return s, nil
}

// reestablish declares the exchange, queue and binding on a pooled channel
// and installs the new delivery source.
func (s *subscription) reestablish() error {
	ch, err := s.bus.subChannel(s.name)
	if err != nil {
		return err
	}
	if err = ch.ExchangeDeclare(s.name, "fanout", false, false, false, false, nil); err != nil {
		return err
	}
	var q amqp.Queue
	if s.queue {
		// Shared named queue: the broker round-robins deliveries between
		// consumers, giving competing-consumer semantics across processes.
		// autoDelete reclaims it once the last consumer detaches.
		q, err = ch.QueueDeclare(s.name, false, true, false, false, nil)
	} else {
		// Exclusive anonymous queue: every subscriber gets its own copy.
		q, err = ch.QueueDeclare("", false, true, true, false, nil)
	}
	if err != nil {
		return err
	}
	if err = ch.QueueBind(q.Name, s.name, s.name, false, nil); err != nil {
		return err
	}
	deliveries, err := ch.Consume(q.Name, s.tag, true, false, false, false, nil)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		return amqp.ErrClosed
	}
	s.ch = ch
	s.deliveries = deliveries
	s.mu.Unlock()
	return nil
}

func (s *subscription) source() <-chan amqp.Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deliveries
}

// pump is the only sender on msgs and the only closer of it, so Close can
// wait on done and hand back a subscription whose channel is already closed.
// Whenever the delivery source is missing or dies — before the first
// connection, after a connection loss, or after a channel-level cancel — the
// pump re-establishes the subscription itself with backoff, so no external
// recovery pass can close or strand it.
func (s *subscription) pump() {
	defer func() {
		s.once.Do(func() { close(s.msgs) })
		close(s.done)
	}()
	for {
		d := s.source()
		if d == nil {
			if !s.heal() {
				return
			}
			continue
		}

		for {
			var (
				msg amqp.Delivery
				ok  bool
			)
			select {
			case msg, ok = <-d:
				if !ok {
					goto sourceLost
				}
			case <-s.ctx.Done():
				return
			}

			if s.sink != nil {
				s.sink(msg.Body)
				continue
			}
			select {
			case s.msgs <- msg.Body:
			case <-s.ctx.Done():
				return
			}
		}

	sourceLost:
		// The source can only be replaced under s.mu by reestablish, which
		// only this pump calls after start-up, so clearing it here cannot
		// race a concurrent install.
		s.mu.Lock()
		s.deliveries = nil
		s.mu.Unlock()
	}
}

// heal re-establishes the subscription with backoff until a delivery source
// is installed or the subscription is closed. It returns false once the
// subscription is done.
func (s *subscription) heal() bool {
	backoff := time.Millisecond * 100
	for {
		if s.closed.Load() || s.ctx.Err() != nil {
			return false
		}
		if err := s.reestablish(); err == nil {
			return true
		}
		select {
		case <-time.After(backoff):
			if backoff < 5*time.Second {
				backoff *= 2
			}
		case <-s.ctx.Done():
			return false
		}
	}
}

func (s *subscription) Read() ([]byte, bool) {
	b, ok := <-s.msgs
	return b, ok
}

func (s *subscription) Close() error {
	// A routed reader owns no broker state: unwinding the router entry (and,
	// with the last one, the underlying subscription) is the whole close.
	if s.r != nil {
		s.bus.removeRouterSub(s)
		return nil
	}

	s.closed.Store(true)
	s.cancel()
	<-s.done
	s.bus.removeSub(s)
	// Cancel the consumer so an exclusive queue with autoDelete is reclaimed
	// instead of lingering on the shared channel until the next reconnect.
	// The channel itself is pooled and shared: never closed here.
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	if ch != nil && !ch.IsClosed() {
		return ch.Cancel(s.tag, false)
	}
	return nil
}

// ---- router: Local demux on one Server subject ----

// router fans one broker subscription out to per-Local subscriptions,
// following the natsbus pattern: the Server subject is shared by every
// logical channel, and Channel.Local is carried in the payload
// (bus.DecodeLocalChannel).
type router struct {
	t       *transport
	subject string
	queue   bool

	// entries and sub are guarded by the bus mutex: both are mutated from
	// subscribe/unsubscribe paths.
	entries map[string][]*subscription // Local channel -> subs
	sub     *subscription              // the one underlying broker subscription
}

// subscribeRouter registers a Local channel on the router for its Server
// subject. The router owns exactly one underlying broker subscription
// (sub.sink feeds r.write); the readers returned here carry no broker state
// of their own.
func (b *transport) subscribeRouter(ctx context.Context, channel bus.Channel, size int, queue bool) (bus.Reader, error) {
	subCtx, cancel := context.WithCancel(ctx)
	subject := channel.Server
	local := channel.Local
	sub := &subscription{
		bus:    b,
		ctx:    subCtx,
		cancel: cancel,
		msgs:   make(chan []byte, size),
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return nil, amqp.ErrClosed
	}
	r, ok := b.routers[subject]
	if !ok {
		r = &router{
			t:       b,
			subject: subject,
			queue:   queue,
			entries: map[string][]*subscription{},
		}
		b.routers[subject] = r
	} else if r.queue != queue {
		b.mu.Unlock()
		cancel()
		return nil, errors.New("psrpc: amqpbus subscription type mismatch for subject " + subject)
	}
	r.entries[local] = append(r.entries[local], sub)
	sub.r = r
	sub.local = local
	b.mu.Unlock()

	if err := b.ensureRouterSub(r); err != nil {
		b.mu.Lock()
		b.removeRouterEntry(r, local, sub)
		b.mu.Unlock()
		cancel()
		return nil, err
	}
	return sub, nil
}

// ensureRouterSub starts the router's underlying broker subscription. While
// the broker is down it is deferred: the subscription's pump re-establishes
// itself with backoff once the bus redialed, mirroring plain subscribes.
func (b *transport) ensureRouterSub(r *router) error {
	b.mu.Lock()
	started := r.sub != nil
	closed := b.closed
	b.mu.Unlock()
	if started || closed {
		if closed && !started {
			return amqp.ErrClosed
		}
		return nil
	}

	subCtx, cancel := context.WithCancel(b.ctx)
	s := &subscription{
		bus:    b,
		name:   exchangeName(r.subject),
		queue:  r.queue,
		ctx:    subCtx,
		cancel: cancel,
		// consumer tags are unique per channel and the channel is shared, so
		// two subscriptions to the same topic must not share a tag
		tag:   "psrpc-" + rand.NewString(),
		msgs:  make(chan []byte),
		done:  make(chan struct{}),
		sink:  r.write,
	}

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		cancel()
		return amqp.ErrClosed
	}
	r.sub = s
	b.mu.Unlock()

	if err := s.reestablish(); err != nil {
		// While the broker is down the subscription is still accepted: the
		// pump keeps retrying and deliveries start once the bus redialed.
		// Any other failure (ACL, precondition) is surfaced to the caller.
		if !errors.Is(err, amqp.ErrClosed) || !b.connectionDown() {
			b.mu.Lock()
			r.sub = nil
			b.mu.Unlock()
			cancel()
			return err
		}
	}
	go s.pump()
	return nil
}

// write demultiplexes one inbound payload by its Local channel. The router's
// underlying subscription feeds it through its sink.
func (r *router) write(payload []byte) {
	local, err := bus.DecodeLocalChannel(payload)
	if err != nil {
		return
	}
	r.t.mu.Lock()
	subs := make([]*subscription, len(r.entries[local]))
	copy(subs, r.entries[local])
	r.t.mu.Unlock()
	for _, sub := range subs {
		sub.write(payload)
	}
}

// write delivers a payload to this subscription's channel.
func (s *subscription) write(payload []byte) {
	select {
	case s.msgs <- payload:
	case <-s.ctx.Done():
	}
}

// removeRouterEntry requires the bus mutex to be held. It returns the
// router's underlying subscription when the router drained and was removed,
// so the caller can stop it outside the lock.
func (b *transport) removeRouterEntry(r *router, local string, sub *subscription) *subscription {
	subs := r.entries[local]
	i := indexOf(subs, sub)
	if i == -1 {
		return nil
	}
	subs = append(subs[:i:i], subs[i+1:]...)
	if len(subs) == 0 {
		delete(r.entries, local)
	} else {
		r.entries[local] = subs
	}
	if len(r.entries) == 0 {
		if cur, ok := b.routers[r.subject]; ok && cur == r {
			delete(b.routers, r.subject)
			return r.sub
		}
	}
	return nil
}

// removeRouterSub unwinds a routed reader: cancel its context, drop it from
// the router, and stop the underlying broker subscription once the router
// has drained.
func (b *transport) removeRouterSub(sub *subscription) {
	sub.cancel()
	b.mu.Lock()
	var under *subscription
	if r := sub.r; r != nil {
		under = b.removeRouterEntry(r, sub.local, sub)
	}
	b.mu.Unlock()
	if under != nil {
		_ = under.Close()
	}
}

// all is used by transport.Close: the routed readers plus the underlying
// subscription, so every pump exits.
func (r *router) all() []*subscription {
	subs := make([]*subscription, 0, 8)
	for _, s := range r.entries {
		subs = append(subs, s...)
	}
	if r.sub != nil {
		subs = append(subs, r.sub)
	}
	return subs
}

func indexOf(subs []*subscription, sub *subscription) int {
	for i, s := range subs {
		if s == sub {
			return i
		}
	}
	return -1
}
