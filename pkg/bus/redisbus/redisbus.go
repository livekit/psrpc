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

// Package redisbus provides a psrpc bus backed by Redis pub/sub.
package redisbus

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"math/rand"
	"net"
	"sync"
	"time"

	"github.com/gammazero/deque"
	"github.com/redis/go-redis/v9"
	"github.com/zeebo/xxh3"
	"go.uber.org/multierr"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/pkg/bus"
)

const (
	lockExpiration          = time.Second * 5
	reconcilerRetryInterval = time.Second
	minReadRetryInterval    = time.Millisecond * 100
	maxReadRetryInterval    = time.Second
	publishBuckets          = 17

	// A subscription only reads, so a socket that has stopped delivering (dropped
	// without a FIN, or left unauthenticated once the credentials it connected with
	// expire) looks exactly like a quiet one. A read that sees nothing for this long
	// sends a PING, and a PING still unanswered after another interval replaces the
	// subscription.
	defaultRedisHealthCheckInterval = 30 * time.Second
)

var errRedisPingUnanswered = errors.New("redis subscription did not answer a ping")

type transport struct {
	rc  redis.UniversalClient
	ctx context.Context

	// psMu keeps a replacement of ps from interleaving with the reconciler, so the
	// replacement subscribes exactly currentChannels. ps is written holding psMu and
	// mu, and read holding either.
	psMu                sync.Mutex
	ps                  *redis.PubSub
	healthCheckInterval time.Duration

	mu     sync.Mutex
	subs   map[string]*redisSubList
	queues map[string]*redisSubList

	wakeup          chan struct{}
	ops             *redisWriteOpQueue
	dirtyChannels   map[string]struct{}
	currentChannels map[string]struct{}

	publishQueues [publishBuckets]*redisPublishQueue
}

// rc is borrowed, not owned: closing it is the caller's job.
func New(rc redis.UniversalClient, opts ...bus.BusOption) bus.MessageBus {
	return newBus(rc, defaultRedisHealthCheckInterval, opts...)
}

func newBus(rc redis.UniversalClient, healthCheckInterval time.Duration, opts ...bus.BusOption) bus.MessageBus {
	ctx := context.Background()
	r := &transport{
		rc:                  rc,
		ctx:                 ctx,
		ps:                  rc.Subscribe(ctx),
		healthCheckInterval: healthCheckInterval,
		subs:                map[string]*redisSubList{},
		queues:              map[string]*redisSubList{},

		wakeup:          make(chan struct{}, 1),
		ops:             &redisWriteOpQueue{},
		dirtyChannels:   map[string]struct{}{},
		currentChannels: map[string]struct{}{},
	}
	for i := range len(r.publishQueues) {
		r.publishQueues[i] = newRedisPublishQueue(r.ctx, r.rc)
	}
	go r.readWorker()
	go r.writeWorker()
	return bus.New(r, opts...)
}

func (r *transport) Publish(_ context.Context, channel bus.Channel, b []byte) error {
	bucket := xxh3.HashString(channel.Legacy) % publishBuckets
	r.publishQueues[bucket].Enqueue(channel.Legacy, b)
	return nil
}

func (r *transport) Subscribe(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	return r.subscribe(ctx, channel.Legacy, size, r.subs, false)
}

func (r *transport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	return r.subscribe(ctx, channel.Legacy, size, r.queues, true)
}

func (r *transport) subscribe(ctx context.Context, channel string, size int, subLists map[string]*redisSubList, queue bool) (bus.Reader, error) {
	ctx, cancel := context.WithCancel(ctx)
	sub := &redisSubscription{
		bus:     r,
		ctx:     ctx,
		cancel:  cancel,
		channel: channel,
		msgChan: make(chan *redis.Message, size),
		queue:   queue,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	subList, ok := subLists[channel]
	if !ok {
		subList = &redisSubList{}
		subLists[channel] = subList
		r.reconcileSubscriptions(channel)
	}
	subList.subs = append(subList.subs, sub)

	return sub, nil
}

func (r *transport) unsubscribe(channel string, queue bool, sub *redisSubscription) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var subLists map[string]*redisSubList
	if queue {
		subLists = r.queues
	} else {
		subLists = r.subs
	}

	subList, ok := subLists[channel]
	if !ok {
		return
	}
	i := slices.Index(subList.subs, sub)
	if i == -1 {
		return
	}

	subList.subs = slices.Delete(subList.subs, i, i+1)

	if len(subList.subs) == 0 {
		delete(subLists, channel)
		r.reconcileSubscriptions(channel)
	}
}

func (r *transport) readWorker() {
	var delay time.Duration
	backoff := func() {
		time.Sleep(delay)
		if delay *= 2; delay == 0 {
			delay = minReadRetryInterval
		} else if delay > maxReadRetryInterval {
			delay = maxReadRetryInterval
		}
	}

	var pinged bool
	for {
		r.mu.Lock()
		ps := r.ps
		r.mu.Unlock()

		reply, err := ps.ReceiveTimeout(r.ctx, r.healthCheckInterval)
		if err != nil {
			switch {
			case r.isReplaced(ps):
				// The read ended because the subscription it was on was closed.
				pinged = false
			case isNetTimeout(err) && !pinged:
				pinged = r.ping(ps)
			case isNetTimeout(err):
				r.replaceSubscription(ps, errRedisPingUnanswered)
				pinged = false
			case isRedisErrorReply(err):
				// go-redis keeps a connection that answers with an error, but a
				// subscription that errors (NOAUTH once its credentials lapse) is no
				// longer one Redis delivers to.
				r.replaceSubscription(ps, err)
				pinged = false
				backoff()
			default:
				logger.Error(err, "redis receive message failed")
				backoff()
			}
			continue
		}
		delay = 0
		pinged = false

		msg, ok := reply.(*redis.Message)
		if !ok {
			continue
		}

		r.mu.Lock()
		if subList, ok := r.subs[msg.Channel]; ok {
			subList.dispatch(msg)
		}
		if subList, ok := r.queues[msg.Channel]; ok {
			subList.dispatchQueue(msg)
		}
		r.mu.Unlock()
	}
}

func (r *transport) isReplaced(ps *redis.PubSub) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ps != ps
}

func (r *transport) ping(ps *redis.PubSub) bool {
	if err := ps.Ping(r.ctx); err != nil {
		logger.Error(err, "redis subscription ping failed")
		return false
	}
	return true
}

// replaceSubscription swaps ps for a new PubSub, and so a new connection, subscribed
// to every channel currently subscribed.
func (r *transport) replaceSubscription(old *redis.PubSub, reason error) {
	logger.Error(reason, "redis subscription unhealthy, resubscribing")

	r.psMu.Lock()
	r.mu.Lock()
	if r.ps != old {
		r.mu.Unlock()
		r.psMu.Unlock()
		return
	}
	channels := maps.Keys(r.currentChannels)
	r.mu.Unlock()

	ps := r.rc.Subscribe(r.ctx, channels...)

	r.mu.Lock()
	r.ps = ps
	r.mu.Unlock()
	r.psMu.Unlock()

	_ = old.Close()
}

func isNetTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isRedisErrorReply(err error) bool {
	var redisErr redis.Error
	return errors.As(err, &redisErr)
}

func (r *transport) reconcileSubscriptions(channel string) {
	r.dirtyChannels[channel] = struct{}{}
	r.enqueueWriteOp(&redisReconcileSubscriptionsOp{r})
}

func (r *transport) enqueueWriteOp(op redisWriteOp) {
	r.ops.push(op)
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *transport) writeWorker() {
	for range r.wakeup {
		r.ops.drain()
	}
}

// ----------------------------------------------

type redisWriteOpQueue struct {
	mu  sync.Mutex
	ops deque.Deque[redisWriteOp]
}

func (q *redisWriteOpQueue) empty() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.ops.Len() == 0
}

func (q *redisWriteOpQueue) push(op redisWriteOp) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.ops.PushBack(op)
}

func (q *redisWriteOpQueue) drain() {
	q.mu.Lock()
	for q.ops.Len() > 0 {
		op := q.ops.PopFront()
		q.mu.Unlock()
		if err := op.run(); err != nil {
			logger.Error(err, "redis write message failed")
		}
		q.mu.Lock()
	}
	q.mu.Unlock()
}

//-----------------------------------------------------

type redisWriteOp interface {
	run() error
}

// ----------------------------------------------------

type redisReconcileSubscriptionsOp struct {
	*transport
}

func (r *redisReconcileSubscriptionsOp) run() error {
	r.mu.Lock()
	for len(r.dirtyChannels) > 0 {
		subscribe := make(map[string]struct{}, len(r.dirtyChannels))
		unsubscribe := make(map[string]struct{}, len(r.dirtyChannels))
		for c := range r.dirtyChannels {
			_, current := r.currentChannels[c]
			desired := r.subs[c] != nil || r.queues[c] != nil
			if !current && desired {
				subscribe[c] = struct{}{}
			} else if current && !desired {
				unsubscribe[c] = struct{}{}
			}
		}
		maps.Clear(r.dirtyChannels)
		r.mu.Unlock()

		// Held until currentChannels reflects this change, so a replacement of ps
		// cannot subscribe a set that is missing it.
		r.psMu.Lock()
		var subscribeErr, unsubscribeErr error
		if len(subscribe) != 0 {
			subscribeErr = r.ps.Subscribe(r.ctx, maps.Keys(subscribe)...)
		}
		if len(unsubscribe) != 0 {
			unsubscribeErr = r.ps.Unsubscribe(r.ctx, maps.Keys(unsubscribe)...)
		}

		r.mu.Lock()
		if subscribeErr != nil {
			maps.Copy(r.dirtyChannels, subscribe)
		} else {
			maps.Copy(r.currentChannels, subscribe)
		}
		if unsubscribeErr != nil {
			maps.Copy(r.dirtyChannels, unsubscribe)
		} else {
			for c := range unsubscribe {
				delete(r.currentChannels, c)
			}
		}
		r.mu.Unlock()
		r.psMu.Unlock()

		if err := multierr.Combine(subscribeErr, unsubscribeErr); err != nil {
			logger.Error(err, "redis subscription reconciliation failed")
			time.Sleep(reconcilerRetryInterval)
		}

		r.mu.Lock()
	}
	r.mu.Unlock()
	return nil
}

// ----------------------------------------------------

type redisSubList struct {
	subs []*redisSubscription
	next int
}

func (r *redisSubList) dispatchQueue(msg *redis.Message) {
	if r.next >= len(r.subs) {
		r.next = 0
	}
	r.subs[r.next].write(msg)
	r.next++
}

func (r *redisSubList) dispatch(msg *redis.Message) {
	for _, sub := range r.subs {
		sub.write(msg)
	}
}

// ----------------------------------------------------

type redisSubscription struct {
	bus     *transport
	ctx     context.Context
	cancel  context.CancelFunc
	channel string
	msgChan chan *redis.Message
	queue   bool
}

func (r *redisSubscription) write(msg *redis.Message) {
	select {
	case r.msgChan <- msg:
	case <-r.ctx.Done():
	}
}

func (r *redisSubscription) Read() ([]byte, bool) {
	for {
		var msg *redis.Message
		var ok bool
		select {
		case msg, ok = <-r.msgChan:
			if !ok {
				return nil, false
			}
		case <-r.ctx.Done():
			return nil, false
		}

		if r.queue {
			sha := sha256.Sum256([]byte(msg.Payload))
			hash := base64.StdEncoding.EncodeToString(sha[:])
			acquired, err := r.bus.rc.SetNX(r.ctx, hash, rand.Int(), lockExpiration).Result()
			if err != nil || !acquired {
				continue
			}
		}

		return []byte(msg.Payload), true
	}
}

func (r *redisSubscription) Close() error {
	r.cancel()
	r.bus.unsubscribe(r.channel, r.queue, r)
	close(r.msgChan)
	return nil
}

// ----------------------------------------------------

type redisPublishMessage struct {
	channel string
	payload []byte
}

type redisPublishQueue struct {
	ctx context.Context
	rc  redis.UniversalClient

	lock     sync.Mutex
	messages []redisPublishMessage
	wakeup   chan struct{}
}

func newRedisPublishQueue(ctx context.Context, rc redis.UniversalClient) *redisPublishQueue {
	r := &redisPublishQueue{
		ctx:    ctx,
		rc:     rc,
		wakeup: make(chan struct{}, 1),
	}

	go r.worker()
	return r
}

func (r *redisPublishQueue) Enqueue(channel string, payload []byte) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.messages = append(r.messages, redisPublishMessage{channel, payload})
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *redisPublishQueue) worker() {
	for {
		select {
		case <-r.wakeup:
		case <-r.ctx.Done():
			return
		}

		r.lock.Lock()
		messages := r.messages
		r.messages = nil
		r.lock.Unlock()

		// using a pipeline to handle redis servers with a high RTT
		// (https://redis.io/docs/latest/develop/using-commands/pipelining/).
		//
		// This is doing oppotunistic batching + pipelining.
		// When a message is published, this worker is signalled immediately and
		// the message will be sent immediately. While the pipeline is executing,
		// messages will get queued as the pipeline execution takes one round trip
		// exchange with the server. The next round will batch all those queued
		// messages. For small RTTs, this will send messages without any delay.
		pipeline := r.rc.Pipeline()
		for _, msg := range messages {
			pipeline.Publish(r.ctx, msg.channel, msg.payload)
		}
		cmds, err := pipeline.Exec(r.ctx)
		if err != nil {
			logger.Error(err, "pipeline execution failed", "numCommands", len(cmds))
		}
	}
}

// ----------------------------------------------------
