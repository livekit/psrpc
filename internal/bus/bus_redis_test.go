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
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestRedisMessageBus(t *testing.T) {
	t.Run("published messages are received by subscribers", func(t *testing.T) {
		rc0 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		rc1 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		t.Cleanup(func() {
			rc0.Close()
			rc1.Close()
		})

		b0 := NewRedisMessageBus(rc0)
		b1 := NewRedisMessageBus(rc1)

		r, err := b0.Subscribe(context.Background(), "test", 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b1.Publish(context.Background(), "test", src)
		require.NoError(t, err)

		b, ok := r.read()
		require.True(t, ok)

		dst, err := deserialize(b)
		require.NoError(t, err)
		require.Equal(t, src.Value, dst.(*wrapperspb.StringValue).Value)
	})

	t.Run("published messages are received by only one queue subscriber", func(t *testing.T) {
		rc0 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		rc1 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		rc2 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		t.Cleanup(func() {
			rc0.Close()
			rc1.Close()
			rc2.Close()
		})

		b0 := NewRedisMessageBus(rc0)
		b1 := NewRedisMessageBus(rc1)
		b2 := NewRedisMessageBus(rc2)

		r1, err := b1.SubscribeQueue(context.Background(), "test", 100)
		require.NoError(t, err)
		r2, err := b2.SubscribeQueue(context.Background(), "test", 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), "test", src)
		require.NoError(t, err)

		var n atomic.Int64

		go func() {
			if _, ok := r1.read(); ok {
				n.Inc()
			}
		}()
		go func() {
			if _, ok := r2.read(); ok {
				n.Inc()
			}
		}()

		time.Sleep(time.Second)

		require.EqualValues(t, 1, n.Load())
	})

	t.Run("closed subscriptions are unreadable", func(t *testing.T) {
		rc0 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		rc1 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		rc2 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		t.Cleanup(func() {
			rc0.Close()
			rc1.Close()
			rc2.Close()
		})

		b0 := NewRedisMessageBus(rc0)
		b1 := NewRedisMessageBus(rc1)
		b2 := NewRedisMessageBus(rc2)

		r1, err := b1.Subscribe(context.Background(), "test", 100)
		require.NoError(t, err)
		r2, err := b2.Subscribe(context.Background(), "test", 100)
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		src := wrapperspb.String("test")

		err = b0.Publish(context.Background(), "test", src)
		require.NoError(t, err)

		_, ok := r1.read()
		require.True(t, ok)
		_, ok = r2.read()
		require.True(t, ok)

		err = r1.Close()
		require.NoError(t, err)

		time.Sleep(time.Second)

		err = b0.Publish(context.Background(), "test", src)
		require.NoError(t, err)

		_, ok = r1.read()
		require.False(t, ok)
		_, ok = r2.read()
		require.True(t, ok)
	})
}

func BenchmarkRedisMessageBus(b *testing.B) {
	rc0 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
	rc1 := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
	b.Cleanup(func() {
		rc0.Close()
		rc1.Close()
	})

	b0 := NewRedisMessageBus(rc0)
	b1 := NewRedisMessageBus(rc1)

	r, _ := b0.Subscribe(context.Background(), "test", 100)

	time.Sleep(100 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		for i := 0; i < b.N; i++ {
			r.read()
		}
		close(done)
	}()

	b.ResetTimer()

	src := wrapperspb.String("test")
	for i := 0; i < b.N; i++ {
		b1.Publish(context.Background(), "test", src)
	}

	<-done
}
