package psrpc

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

func (l *localMessageBus) Publish(_ context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	l.RLock()
	subs := l.subs[channel]
	queues := l.queues[channel]
	l.RUnlock()

	if subs != nil {
		subs.publish(b)
	}
	if queues != nil {
		queues.publish(b)
	}
	return nil
}

func (l *localMessageBus) Subscribe(_ context.Context, channel string, size int) (subInternal, error) {
	msgChan := make(chan []byte, size)

	l.Lock()
	defer l.Unlock()

	subs := l.subs[channel]
	if subs == nil {
		subs = &localSubList{}
		subs.onUnsubscribe = func() {
			// lock localMessageBus before localSubList
			l.Lock()
			subs.Lock()
			if subs.subCount == 0 {
				delete(l.subs, channel)
			}
			subs.Unlock()
			l.Unlock()
		}
		l.subs[channel] = subs
	}

	sub := &localSubscription{msgChan: msgChan}
	subs.add(sub)

	return sub, nil
}

func (l *localMessageBus) SubscribeQueue(_ context.Context, channel string, size int) (subInternal, error) {
	msgChan := make(chan []byte, size)

	l.Lock()
	defer l.Unlock()

	queue := l.queues[channel]
	if queue == nil {
		queue = &localSubList{
			queue: true,
		}
		queue.onUnsubscribe = func() {
			// lock localMessageBus before localSubList
			l.Lock()
			queue.Lock()
			if queue.subCount == 0 {
				delete(l.queues, channel)
			}
			queue.Unlock()
			l.Unlock()
		}
		l.queues[channel] = queue
	}

	sub := &localSubscription{msgChan: msgChan}
	queue.add(sub)

	return sub, nil
}

type localSubList struct {
	sync.RWMutex  // locking while holding localMessageBus lock is allowed
	subs          []chan []byte
	subCount      int
	queue         bool
	next          int
	onUnsubscribe func()
}

func (l *localSubList) add(sub *localSubscription) {
	l.Lock()
	defer l.Unlock()

	l.subCount++
	added := false
	index := 0
	for i, s := range l.subs {
		if s == nil {
			added = true
			index = i
			l.subs[i] = sub.msgChan
			break
		}
	}

	if !added {
		index = len(l.subs)
		l.subs = append(l.subs, sub.msgChan)
	}

	sub.onClose = func() {
		l.Lock()
		l.subCount--
		close(l.subs[index])
		l.subs[index] = nil
		l.Unlock()
		// call after unlocking, since onUnsubscribe locks localMessageBus
		l.onUnsubscribe()
	}
}

func (l *localSubList) publish(b []byte) {
	if l.queue {
		l.Lock()
		defer l.Unlock()

		for i := 0; i <= len(l.subs); i++ {
			if l.next >= len(l.subs) {
				l.next = 0
			}
			s := l.subs[l.next]
			l.next++
			if s != nil {
				s <- b
				return
			}
		}
	} else {
		l.RLock()
		defer l.RUnlock()

		for _, s := range l.subs {
			if s != nil {
				s <- b
			}
		}
	}
}

type localSubscription struct {
	msgChan chan []byte
	onClose func()
}

func (l *localSubscription) read() ([]byte, bool) {
	msg, ok := <-l.msgChan
	if !ok {
		return nil, false
	}
	return msg, true
}

func (l *localSubscription) Close() error {
	l.onClose()
	return nil
}
