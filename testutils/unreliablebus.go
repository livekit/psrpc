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
	"context"
	"math/rand"

	"go.uber.org/atomic"
)

type AtomicFailureRate struct {
	v *atomic.Float64
}

func NewAtomicFailureRate(v float64) AtomicFailureRate {
	return AtomicFailureRate{v: atomic.NewFloat64(v)}
}

func (g *AtomicFailureRate) Rate() float64 {
	return g.v.Load()
}

func (g *AtomicFailureRate) SetRate(v float64) {
	g.v.Store(v)
}

func WithUnreliableBus(rate AtomicFailureRate) TestBusOption {
	return WithUnreliableBusChannel(rate, "")
}

func WithUnreliableBusChannel(rate AtomicFailureRate, channelFilter string) TestBusOption {
	return WithSubscribeInterceptor(func(_ context.Context, channel string, next ReadHandler) ReadHandler {
		if channelFilter != "" && channelFilter != channel {
			return next
		}

		rng := rand.New(rand.NewSource(0))
		return func() ([]byte, bool) {
			for {
				b, ok := next()
				if ok && rate.Rate() > rng.Float64() {
					continue
				}
				return b, ok
			}
		}
	})
}
