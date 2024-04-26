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
	"sync"

	"google.golang.org/protobuf/proto"
)

type localMessageBus struct {
	sync.RWMutex
	subs   map[string]*localSubList
	queues map[string]*localSubList
}

func NewLocalMessageBus() MessageBus {
	return &localMessageBus{
		subs:   make(map[string]*localSubList),
		queues: make(map[string]*localSubList),
	}
}

func (l *localMessageBus) Publish(_ context.Context, channel Channel, msg proto.Message) error {
	b, err := serialize(msg, "")
	if err != nil {
		return err
	}

	l.RLock()
	subs := l.subs[channel.Legacy]
	queues := l.queues[channel.Legacy]
	l.RUnlock()

	if subs != nil {
		subs.dispatch(b)
	}
	if queues != nil {
		queues.dispatch(b)
	}
	return nil
}

func (l *localMessageBus) Subscribe(ctx context.Context, channel Channel, size int) (Reader, error) {
	return l.subscribe(ctx, l.subs, channel.Legacy, size, false)
}

func (l *localMessageBus) SubscribeQueue(ctx context.Context, channel Channel, size int) (Reader, error) {
	return l.subscribe(ctx, l.queues, channel.Legacy, size, true)
}

func (l *localMessageBus) subscribe(ctx context.Context, subLists map[string]*localSubList, channel string, size int, queue bool) (Reader, error) {
	l.Lock()
	defer l.Unlock()

	subList := subLists[channel]
	if subList == nil {
		subList = &localSubList{queue: queue}
		subList.onUnsubscribe = func(index int) {
			// lock localMessageBus before localSubList
			l.Lock()
			subList.Lock()

			subList.subs[index] = nil
			subList.subCount--
			if subList.subCount == 0 {
				delete(subLists, channel)
			}

			subList.Unlock()
			l.Unlock()
		}
		subLists[channel] = subList
	}

	return subList.create(ctx, size), nil
}

type localSubList struct {
	sync.RWMutex  // locking while holding localMessageBus lock is allowed
	subs          []*localSubscription
	subCount      int
	queue         bool
	next          int
	onUnsubscribe func(int)
}

func (l *localSubList) create(ctx context.Context, size int) *localSubscription {
	ctx, cancel := context.WithCancel(ctx)
	sub := &localSubscription{
		ctx:     ctx,
		cancel:  cancel,
		msgChan: make(chan []byte, size),
	}

	l.Lock()
	defer l.Unlock()

	l.subCount++
	added := false
	index := 0
	for i, s := range l.subs {
		if s == nil {
			added = true
			index = i
			l.subs[i] = sub
			break
		}
	}

	if !added {
		index = len(l.subs)
		l.subs = append(l.subs, sub)
	}

	sub.onClose = func() {
		l.onUnsubscribe(index)
	}

	return sub
}

func (l *localSubList) dispatch(b []byte) {
	if l.queue {
		l.Lock()
		defer l.Unlock()

		// round-robin
		for i := 0; i <= len(l.subs); i++ {
			if l.next >= len(l.subs) {
				l.next = 0
			}
			s := l.subs[l.next]
			l.next++
			if s != nil {
				s.write(b)
				return
			}
		}
	} else {
		l.RLock()
		defer l.RUnlock()

		// send to all
		for _, s := range l.subs {
			if s != nil {
				s.write(b)
			}
		}
	}
}

type localSubscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan []byte
	onClose func()
}

func (l *localSubscription) write(b []byte) {
	select {
	case l.msgChan <- b:
	case <-l.ctx.Done():
	}
}

func (l *localSubscription) read() ([]byte, bool) {
	msg, ok := <-l.msgChan
	if !ok {
		return nil, false
	}
	return msg, true
}

func (l *localSubscription) Close() error {
	l.cancel()
	l.onClose()
	close(l.msgChan)
	return nil
}
