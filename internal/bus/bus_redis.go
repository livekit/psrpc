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
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
	"sync"
	"time"

	"github.com/gammazero/deque"
	"github.com/redis/go-redis/v9"
	"go.uber.org/multierr"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal/logger"
)

const lockExpiration = time.Second * 5
const reconcilerRetryInterval = time.Second

type redisMessageBus struct {
	rc  redis.UniversalClient
	ctx context.Context
	ps  *redis.PubSub

	mu     sync.Mutex
	subs   map[string]*redisSubList
	queues map[string]*redisSubList

	wakeup          chan struct{}
	ops             *deque.Deque[redisWriteOp]
	dirtyChannels   *deque.Deque[string]
	currentChannels map[string]struct{}
}

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	ctx := context.Background()
	r := &redisMessageBus{
		rc:     rc,
		ctx:    ctx,
		ps:     rc.Subscribe(ctx),
		subs:   map[string]*redisSubList{},
		queues: map[string]*redisSubList{},

		wakeup:          make(chan struct{}, 1),
		ops:             deque.New[redisWriteOp](),
		dirtyChannels:   deque.New[string](),
		currentChannels: map[string]struct{}{},
	}
	go r.readWorker()
	go r.writeWorker()
	return r
}

func (r *redisMessageBus) Publish(_ context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.enqueueWriteOp(&redisPublishOp{r, channel, b})
	r.mu.Unlock()
	return nil
}

func (r *redisMessageBus) Subscribe(ctx context.Context, channel string, size int) (Reader, error) {
	return r.subscribe(ctx, channel, size, r.subs, false)
}

func (r *redisMessageBus) SubscribeQueue(ctx context.Context, channel string, size int) (Reader, error) {
	return r.subscribe(ctx, channel, size, r.queues, true)
}

func (r *redisMessageBus) subscribe(ctx context.Context, channel string, size int, subLists map[string]*redisSubList, queue bool) (Reader, error) {
	sub := &redisSubscription{
		bus:     r,
		ctx:     ctx,
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
	subList.subs = append(subList.subs, sub.msgChan)

	return sub, nil
}

func (r *redisMessageBus) unsubscribe(channel string, queue bool, msgChan chan *redis.Message) {
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
	i := slices.Index(subList.subs, msgChan)
	if i == -1 {
		return
	}

	subList.subs = slices.Delete(subList.subs, i, i+1)
	close(msgChan)

	if len(subList.subs) == 0 {
		delete(subLists, channel)
		r.reconcileSubscriptions(channel)
	}
}

func (r *redisMessageBus) readWorker() {
	for {
		msg, err := r.ps.ReceiveMessage(r.ctx)
		if err != nil {
			return
		}

		r.mu.Lock()
		if subList, ok := r.subs[msg.Channel]; ok {
			subList.publish(msg)
		}
		if subList, ok := r.queues[msg.Channel]; ok {
			subList.publishQueue(msg)
		}
		r.mu.Unlock()
	}
}

func (r *redisMessageBus) reconcileSubscriptions(channel string) {
	r.dirtyChannels.PushBack(channel)
	r.enqueueWriteOp(&redisReconcileSubscriptionsOp{r})
}

func (r *redisMessageBus) enqueueWriteOp(op redisWriteOp) {
	r.ops.PushBack(op)
	select {
	case r.wakeup <- struct{}{}:
	default:
	}
}

func (r *redisMessageBus) writeWorker() {
	for range r.wakeup {
		r.mu.Lock()
		for r.ops.Len() > 0 {
			op := r.ops.PopFront()
			r.mu.Unlock()
			op.run()
			r.mu.Lock()
		}
		r.mu.Unlock()
	}
}

type redisWriteOp interface {
	run()
}

type redisPublishOp struct {
	*redisMessageBus
	channel string
	message []byte
}

func (r *redisPublishOp) run() {
	r.rc.Publish(r.ctx, r.channel, r.message)
}

type redisReconcileSubscriptionsOp struct {
	*redisMessageBus
}

func (r *redisReconcileSubscriptionsOp) run() {
	r.mu.Lock()
	for r.dirtyChannels.Len() > 0 {
		subscribe := map[string]struct{}{}
		unsubscribe := map[string]struct{}{}
		for r.dirtyChannels.Len() > 0 {
			c := r.dirtyChannels.PopFront()
			_, current := r.currentChannels[c]
			desired := r.subs[c] != nil || r.queues[c] != nil
			if !current && desired {
				delete(unsubscribe, c)
				subscribe[c] = struct{}{}
			} else if current && !desired {
				delete(subscribe, c)
				unsubscribe[c] = struct{}{}
			}
		}
		r.mu.Unlock()

		var subscribeErr, unsubscribeErr error
		if len(subscribe) != 0 {
			subscribeErr = r.ps.Subscribe(r.ctx, maps.Keys(subscribe)...)
		}
		if len(unsubscribe) != 0 {
			unsubscribeErr = r.ps.Unsubscribe(r.ctx, maps.Keys(unsubscribe)...)
		}

		if err := multierr.Combine(subscribeErr, unsubscribeErr); err != nil {
			logger.Error(err, "redis subscription reconciliation failed")
			time.Sleep(reconcilerRetryInterval)
		}

		r.mu.Lock()
		if subscribeErr != nil {
			for c := range subscribe {
				r.dirtyChannels.PushFront(c)
			}
		} else {
			for c := range subscribe {
				r.currentChannels[c] = struct{}{}
			}
		}
		if unsubscribeErr != nil {
			for c := range unsubscribe {
				r.dirtyChannels.PushFront(c)
			}
		} else {
			for c := range unsubscribe {
				delete(r.currentChannels, c)
			}
		}
	}
	r.mu.Unlock()
}

type redisSubList struct {
	subs []chan *redis.Message
	next int
}

func (r *redisSubList) publishQueue(msg *redis.Message) {
	if r.next > len(r.subs) {
		r.next = 0
	}
	r.subs[r.next] <- msg
	r.next++
}

func (r *redisSubList) publish(msg *redis.Message) {
	for _, ch := range r.subs {
		ch <- msg
	}
}

type redisSubscription struct {
	bus     *redisMessageBus
	ctx     context.Context
	channel string
	msgChan chan *redis.Message
	queue   bool
}

func (r *redisSubscription) read() ([]byte, bool) {
	for {
		var msg *redis.Message
		var ok bool
		select {
		case msg, ok = <-r.msgChan:
			if !ok {
				return nil, false
			}
		case <-r.ctx.Done():
			r.Close()
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
	r.bus.unsubscribe(r.channel, r.queue, r.msgChan)
	return nil
}
