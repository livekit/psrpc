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

// Package localbus provides an in-process psrpc bus.
package localbus

import (
	"context"
	"sync"

	"github.com/livekit/psrpc/pkg/bus"
)

type transport struct {
	sync.RWMutex
	subs   map[string]*subList
	queues map[string]*subList
}

// Only publishers and subscribers sharing the returned value can reach each
// other.
func New(opts ...bus.BusOption) bus.MessageBus {
	return bus.New(&transport{
		subs:   make(map[string]*subList),
		queues: make(map[string]*subList),
	}, opts...)
}

func (l *transport) Publish(_ context.Context, channel bus.Channel, b []byte) error {
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

func (l *transport) Subscribe(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	return l.subscribe(ctx, l.subs, channel.Legacy, size, false)
}

func (l *transport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	return l.subscribe(ctx, l.queues, channel.Legacy, size, true)
}

func (l *transport) subscribe(ctx context.Context, subLists map[string]*subList, channel string, size int, queue bool) (bus.Reader, error) {
	l.Lock()
	defer l.Unlock()

	sl := subLists[channel]
	if sl == nil {
		sl = &subList{queue: queue}
		sl.onUnsubscribe = func(index int) {
			// lock transport before subList
			l.Lock()
			sl.Lock()

			sl.subs[index] = nil
			sl.subCount--
			if sl.subCount == 0 {
				delete(subLists, channel)
			}

			sl.Unlock()
			l.Unlock()
		}
		subLists[channel] = sl
	}

	return sl.create(ctx, size), nil
}

type subList struct {
	sync.RWMutex  // locking while holding transport lock is allowed
	subs          []*subscription
	subCount      int
	queue         bool
	next          int
	onUnsubscribe func(int)
}

func (l *subList) create(ctx context.Context, size int) *subscription {
	ctx, cancel := context.WithCancel(ctx)
	sub := &subscription{
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

func (l *subList) dispatch(b []byte) {
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

type subscription struct {
	ctx     context.Context
	cancel  context.CancelFunc
	msgChan chan []byte
	onClose func()
}

func (l *subscription) write(b []byte) {
	select {
	case l.msgChan <- b:
	case <-l.ctx.Done():
	}
}

func (l *subscription) Read() ([]byte, bool) {
	msg, ok := <-l.msgChan
	if !ok {
		return nil, false
	}
	return msg, true
}

func (l *subscription) Close() error {
	l.cancel()
	l.onClose()
	close(l.msgChan)
	return nil
}
