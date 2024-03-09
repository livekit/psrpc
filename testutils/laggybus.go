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

package testutils

import (
	"container/heap"
	"context"
	"math"
	sync "sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

func WithLaggyBus(id string, latency latencyFunc) TestBusOption {
	return WithBusOptions(
		WithPublishInterceptor(func(next PublishHandler) PublishHandler {
			return func(ctx context.Context, channel Channel, msg proto.Message) error {
				a, err := anypb.New(msg)
				if err != nil {
					return err
				}

				b, err := proto.Marshal(a)
				if err != nil {
					return err
				}

				return next(ctx, channel, &LaggyMessage{
					Origin: id,
					SentAt: time.Now().UnixNano(),
					Body:   b,
				})
			}
		}),
		WithSubscribeInterceptor(func(ctx context.Context, channel Channel, next ReadHandler) ReadHandler {
			l := newLaggySubscribeInterceptor()
			go l.Copy(ctx, id, latency, next)
			return l.Read
		}),
	)
}

type latencyFunc func(a, b string) time.Duration

type laggySubscribeInterceptor struct {
	mu sync.Mutex
	rh readResultHeap
	t  *time.Timer
}

func newLaggySubscribeInterceptor() *laggySubscribeInterceptor {
	return &laggySubscribeInterceptor{
		t: time.NewTimer(math.MaxInt64),
	}
}

func (l *laggySubscribeInterceptor) pushRead(r *readResult) {
	l.mu.Lock()
	defer l.mu.Unlock()
	heap.Push(&l.rh, r)
	if l.rh.Peek() == r && l.t.Stop() {
		l.t.Reset(time.Until(r.time))
	}
}

func (l *laggySubscribeInterceptor) popRead() ([]byte, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	r := heap.Pop(&l.rh).(*readResult)
	if l.rh.Len() > 0 {
		l.t.Reset(time.Until(l.rh.Peek().time))
	} else if r.ok {
		l.t.Reset(math.MaxInt64)
	}
	return r.body, r.ok
}

func (l *laggySubscribeInterceptor) Copy(
	ctx context.Context,
	id string,
	latency latencyFunc,
	read ReadHandler,
) {
	for ctx.Err() == nil {
		b, ok := read()
		if !ok {
			break
		}

		a := &anypb.Any{}
		if proto.Unmarshal(b, a) != nil {
			break
		}

		m := &LaggyMessage{}
		if a.UnmarshalTo(m) != nil {
			break
		}

		l.pushRead(&readResult{
			time.Unix(0, m.SentAt).Add(latency(m.Origin, id)),
			m.Body,
			ok,
		})
	}

	l.pushRead(&readResult{time: time.Now(), ok: false})
}

func (l *laggySubscribeInterceptor) Read() (b []byte, open bool) {
	<-l.t.C
	return l.popRead()
}

type readResult struct {
	time time.Time
	body []byte
	ok   bool
}

type readResultHeap struct {
	v []*readResult
}

func (h *readResultHeap) Peek() *readResult  { return h.v[0] }
func (h *readResultHeap) Len() int           { return len(h.v) }
func (h *readResultHeap) Less(i, j int) bool { return h.v[i].time.Before(h.v[j].time) }
func (h *readResultHeap) Swap(i, j int)      { h.v[i], h.v[j] = h.v[j], h.v[i] }
func (h *readResultHeap) Push(r any)         { h.v = append(h.v, r.(*readResult)) }
func (h *readResultHeap) Pop() any {
	r := h.v[len(h.v)-1]
	h.v[len(h.v)-1] = nil
	h.v = h.v[:len(h.v)-1]
	return r
}
