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

package bus_test

import (
	"context"
	"fmt"
	"testing"
	"time"
	"unsafe"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/bus/bustest"
)

func redisTestChannel(channel string) bus.Channel {
	return bus.Channel{Legacy: channel}
}

func TestRedisMessageBus(t *testing.T) {
	srv := bustest.NewRedis(t, bustest.Docker(t))

	t.Run("published messages are received by subscribers", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)

		r, err := b0.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b1.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		b, ok := bus.RawRead(r)
		require.True(t, ok)

		dst, err := bus.Deserialize(b)
		require.NoError(t, err)
		require.Equal(t, src.Value, dst.(*wrapperspb.StringValue).Value)
	})

	t.Run("published messages are received by only one queue subscriber", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)
		b2 := srv.Connect(t)

		r1, err := b1.SubscribeQueue(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)
		r2, err := b2.SubscribeQueue(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		var n atomic.Int64

		go func() {
			if _, ok := bus.RawRead(r1); ok {
				n.Inc()
			}
		}()
		go func() {
			if _, ok := bus.RawRead(r2); ok {
				n.Inc()
			}
		}()

		time.Sleep(time.Second)

		require.EqualValues(t, 1, n.Load())
	})

	t.Run("closed subscriptions are unreadable", func(t *testing.T) {
		b0 := srv.Connect(t)
		b1 := srv.Connect(t)
		b2 := srv.Connect(t)

		r1, err := b1.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)
		r2, err := b2.Subscribe(context.Background(), redisTestChannel("test"), 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		_, ok := bus.RawRead(r1)
		require.True(t, ok)
		_, ok = bus.RawRead(r2)
		require.True(t, ok)

		err = r1.Close()
		require.NoError(t, err)

		time.Sleep(time.Second)

		err = b0.Publish(context.Background(), redisTestChannel("test"), src)
		require.NoError(t, err)

		_, ok = bus.RawRead(r1)
		require.False(t, ok)
		_, ok = bus.RawRead(r2)
		require.True(t, ok)
	})
}

func BenchmarkRedisMessageBus(b *testing.B) {
	srv := bustest.NewRedis(b, bustest.Docker(b))

	b0 := srv.Connect(b)
	b1 := srv.Connect(b)

	r, _ := b0.Subscribe(context.Background(), redisTestChannel("test"), 100)

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			bus.RawRead(r)
		}
		close(done)
	}()

	b.ResetTimer()

	src := wrapperspb.String("test")
	for i := 0; i < b.N; i++ {
		b1.Publish(context.Background(), redisTestChannel("test"), src)
	}

	<-done
}

func TestFoo(t *testing.T) {
	foo := "asdf"
	fmt.Println(*(*[]byte)(unsafe.Pointer(&foo)))
}
