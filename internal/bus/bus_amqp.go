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
	"hash/fnv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"google.golang.org/protobuf/proto"
)

// pubSlot is one slot of the publisher pool, owning a dedicated broker
// connection. Publishing and delivering contend over connection-level flow
// control in RabbitMQ, so publish traffic is kept on its own connections,
// separate from the subscription connection — the same split the MQTT bus
// applies between its broadcast and queue clients. Slots dial lazily and
// self-heal after a connection loss on their next publish.
//
// Slots are selected by hashing the channel name: AMQP preserves ordering
// per connection only, and psrpc relies on per-channel ordering (e.g. a
// stream's ack must not overtake its close), so all publishes to one
// channel must share a connection. Parallelism comes from different
// channels landing on different connections.
type pubSlot struct {
	mu       sync.Mutex
	conn     *amqp.Connection
	ch       *amqp.Channel
	declared map[string]struct{} // exchanges declared on this channel
}

func (p *pubSlot) channel(url string) (*amqp.Channel, error) {
	if p.ch != nil && !p.ch.IsClosed() {
		return p.ch, nil
	}
	if p.conn == nil || p.conn.IsClosed() {
		conn, err := amqp.Dial(url)
		if err != nil {
			return nil, err
		}
		p.conn = conn
	}
	ch, err := p.conn.Channel()
	if err != nil {
		return nil, err
	}
	p.ch = ch
	// A fresh channel invalidates cached declarations: the previous channel
	// may have died from a not_found after an auto-delete exchange vanished,
	// and re-declaring is always safe.
	p.declared = make(map[string]struct{})
	return ch, nil
}

func (p *pubSlot) drop() {
	if p.ch != nil {
		_ = p.ch.Close()
		p.ch = nil
	}
}

const pubPoolSize = 4

func pubSlotIndex(name string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(name))
	return h.Sum32() % pubPoolSize
}

// amqpMessageBus is an AMQP (RabbitMQ) MessageBus with automatic reconnection:
// it owns the connection, redials with backoff after a connection loss and
// re-establishes every subscription (exchange, queue, binding, consumer) on
// the new connection. Publishes during an outage fail fast with the AMQP
// error, mirroring the Redis bus where requests fail while go-redis
// reconnects; PSRPC's request timeouts absorb that.
//
// Still not production-hardened: no publisher confirms, no metrics.
type amqpMessageBus struct {
	url string

	ctx    context.Context
	cancel context.CancelFunc
	done   <-chan struct{}

	mu      sync.Mutex
	conn    *amqp.Connection // carries all subscriptions
	subs    map[*amqpSubscription]struct{}
	pubPool [pubPoolSize]pubSlot // dedicated publish connections
	closed  bool
	wake    atomic.Pointer[chan struct{}]

}

// NewAmqpMessageBus dials the broker and starts connection supervision.
// TLS (amqps://) and URL query params like heartbeat are handled by amqp.Dial.
func NewAmqpMessageBus(url string) (*amqpMessageBus, error) {
	conn, err := amqp.Dial(url)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &amqpMessageBus{
		url:    url,
		ctx:    ctx,
		cancel: cancel,
		done:   ctx.Done(),
		conn:   conn,
		subs:   map[*amqpSubscription]struct{}{},
	}
	wake := make(chan struct{})
	b.wake.Store(&wake)
	go b.supervise()
	return b, nil
}

// Close stops supervision, closes all subscriptions and the connection.
func (b *amqpMessageBus) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	subs := make([]*amqpSubscription, 0, len(b.subs))
	for s := range b.subs {
		subs = append(subs, s)
	}
	conn := b.conn
	b.mu.Unlock()

	b.cancel()
	for _, s := range subs {
		_ = s.Close()
	}
	for i := range b.pubPool {
		b.pubPool[i].mu.Lock()
		b.pubPool[i].drop()
		if b.pubPool[i].conn != nil {
			_ = b.pubPool[i].conn.Close()
			b.pubPool[i].conn = nil
		}
		b.pubPool[i].mu.Unlock()
	}
	if conn != nil {
		return conn.Close()
	}
	return nil
}

// amqpName maps a psrpc channel to an AMQP exchange/queue name. Channel parts
// are sanitized by psrpc to [0-9A-Za-z_], so '|' only ever appears as a
// delimiter. AMQP names disallow '|' but allow '.', and parts cannot contain
// '.', so the replacement cannot collide.
func amqpName(channel Channel) string {
	return strings.ReplaceAll(channel.Legacy, "|", ".")
}

func (b *amqpMessageBus) Publish(ctx context.Context, channel Channel, msg proto.Message) error {
	name := amqpName(channel)
	payload, err := serialize(msg, "")
	if err != nil {
		return err
	}

	slot := &b.pubPool[pubSlotIndex(name)]
	slot.mu.Lock()
	defer slot.mu.Unlock()
	ch, err := slot.channel(b.url)
	if err != nil {
		return err
	}
	if _, ok := slot.declared[name]; !ok {
		// basic.publish is asynchronous, so the synchronous declare guards
		// against publishing into an auto-delete exchange whose last queue
		// unbound. Cached afterwards: the hot path stays an async write.
		if err = ch.ExchangeDeclare(name, "fanout", false, true, false, false, nil); err != nil {
			slot.drop()
			return err
		}
		slot.declared[name] = struct{}{}
	}
	if err = ch.PublishWithContext(ctx, name, "", false, false, amqp.Publishing{Body: payload}); err != nil {
		slot.drop()
		return err
	}
	return nil
}

func (b *amqpMessageBus) Subscribe(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, amqpName(channel), size, false)
}

func (b *amqpMessageBus) SubscribeQueue(ctx context.Context, channel Channel, size int) (Reader, error) {
	return b.subscribe(ctx, amqpName(channel), size, true)
}

func (b *amqpMessageBus) subscribe(ctx context.Context, name string, size int, queue bool) (*amqpSubscription, error) {
	b.mu.Lock()
	conn := b.conn
	b.mu.Unlock()

	s, err := newAmqpSubscription(ctx, b, conn, name, size, queue)
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

func (b *amqpMessageBus) removeSub(s *amqpSubscription) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// supervise re-establishes the connection whenever it drops.
func (b *amqpMessageBus) supervise() {
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
			b.mu.Lock()
			closed := b.closed
			b.mu.Unlock()
			if closed {
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

// recover redials with backoff and re-establishes every subscription. It
// returns false when the bus was closed while recovering.
func (b *amqpMessageBus) recover() bool {
	backoff := time.Millisecond * 100
	for {
		b.mu.Lock()
		closed := b.closed
		b.mu.Unlock()
		if closed {
			return false
		}

		conn, err := amqp.Dial(b.url)
		if err == nil {
			b.mu.Lock()
			b.conn = conn
			subs := make([]*amqpSubscription, 0, len(b.subs))
			for s := range b.subs {
				subs = append(subs, s)
			}
			b.mu.Unlock()

			for _, s := range subs {
				if err = s.reestablish(conn); err != nil {
					// Surface the failure to the subscriber by closing it,
					// rather than leaving it waiting for a recovery that
					// already happened.
					_ = s.Close()
				}
			}
			// Release subscribers waiting for their delivery source, even if
			// some re-establishment failed: they will see closed channels and
			// surface the error upstream.
			newWake := make(chan struct{})
			old := b.wake.Swap(&newWake)
			close(*old)
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

type amqpSubscription struct {
	bus     *amqpMessageBus
	name    string // mapped exchange name
	queue   bool   // shared competing-consumer queue instead of exclusive
	queueNm string // resolved queue name, re-resolved on reconnect
	size    int

	ctx    context.Context
	cancel context.CancelFunc

	mu         sync.Mutex
	ch         *amqp.Channel
	deliveries <-chan amqp.Delivery

	closed atomic.Bool
	msgs   chan []byte
	done   chan struct{}
	once   sync.Once
}

func newAmqpSubscription(ctx context.Context, b *amqpMessageBus, conn *amqp.Connection, name string, size int, queue bool) (*amqpSubscription, error) {
	subCtx, cancel := context.WithCancel(ctx)
	s := &amqpSubscription{
		bus:    b,
		name:   name,
		queue:  queue,
		size:   size,
		ctx:    subCtx,
		cancel: cancel,
		msgs:   make(chan []byte, size),
		done:   make(chan struct{}),
	}
	if err := s.reestablish(conn); err != nil {
		cancel()
		return nil, err
	}
	go s.pump()
	return s, nil
}

// reestablish declares the exchange, queue and binding on the given
// connection and swaps in the new delivery source.
func (s *amqpSubscription) reestablish(conn *amqp.Connection) error {
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	if err = ch.ExchangeDeclare(s.name, "fanout", false, true, false, false, nil); err != nil {
		_ = ch.Close()
		return err
	}
	var q amqp.Queue
	if s.queue {
		// Shared named queue: the broker round-robins deliveries between
		// consumers, giving competing-consumer semantics across processes.
		q, err = ch.QueueDeclare(s.name, false, true, false, false, nil)
	} else {
		// Exclusive anonymous queue: every subscriber gets its own copy.
		q, err = ch.QueueDeclare("", false, true, true, false, nil)
	}
	if err != nil {
		_ = ch.Close()
		return err
	}
	if err = ch.QueueBind(q.Name, s.name, s.name, false, nil); err != nil {
		_ = ch.Close()
		return err
	}
	deliveries, err := ch.Consume(q.Name, "", true, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return err
	}

	s.mu.Lock()
	if s.closed.Load() {
		s.mu.Unlock()
		_ = ch.Close()
		return amqp.ErrClosed
	}
	s.ch = ch
	s.queueNm = q.Name
	s.deliveries = deliveries
	s.mu.Unlock()
	return nil
}

// pump is the only sender on msgs and the only closer of it, so Close can
// wait on done and hand back a subscription whose channel is already closed.
// A closed delivery source only ends the pump if the subscription itself was
// closed; otherwise it waits for the bus to re-establish it. The delivery
// source is read once per reconnection, not per message.
func (s *amqpSubscription) pump() {
	defer func() {
		s.once.Do(func() { close(s.msgs) })
		close(s.done)
	}()
	for {
		s.mu.Lock()
		d := s.deliveries
		s.mu.Unlock()
		if d == nil {
			return
		}

		for {
			var (
				msg amqp.Delivery
				ok  bool
			)
			select {
			case msg, ok = <-d:
				if !ok {
					goto reestablished
				}
			case <-s.ctx.Done():
				return
			}

			select {
			case s.msgs <- msg.Body:
			case <-s.ctx.Done():
				return
			}
		}

	reestablished:
		if s.closed.Load() {
			return
		}
		// Connection lost: wait for the bus to re-establish this
		// subscription or for the subscription to be closed.
		wake := s.bus.wake.Load()
		select {
		case <-*wake:
		case <-s.ctx.Done():
			return
		}
	}
}

func (s *amqpSubscription) read() ([]byte, bool) {
	b, ok := <-s.msgs
	return b, ok
}

func (s *amqpSubscription) Close() error {
	s.closed.Store(true)
	s.cancel()
	s.mu.Lock()
	ch := s.ch
	s.mu.Unlock()
	var err error
	if ch != nil {
		err = ch.Close()
	}
	<-s.done
	s.bus.removeSub(s)
	return err
}
