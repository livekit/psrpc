// Copyright 2026 LiveKit, Inc.
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

package natsbus

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
)

// A NATS callback that is already running when Close returns still calls write.
func TestSubscriptionWriteAfterClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sub := &subscription{
		ctx:     ctx,
		cancel:  cancel,
		sub:     &nats.Subscription{},
		msgChan: make(chan *nats.Msg, 1),
	}
	_ = sub.Close()

	require.NotPanics(t, func() {
		for range 100 {
			sub.write(&nats.Msg{Data: []byte("late")})
		}
	})

	// Read may still return what is buffered, then reports the close
	buffered := 0
	for {
		if _, ok := sub.Read(); !ok {
			break
		}
		buffered++
	}
	require.LessOrEqual(t, buffered, cap(sub.msgChan))
}
