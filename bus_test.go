package psrpc

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/livekit/pubsub-rpc/internal"
)

func TestMessageBus(t *testing.T) {
	t.Run("Redis", func(t *testing.T) {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		bus := NewRedisMessageBus(rc)
		testSubscribe(t, bus)
		testSubscribeQueue(t, bus)
	})

	t.Run("Nats", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		bus := NewNatsMessageBus(nc)
		testSubscribe(t, bus)
		testSubscribeQueue(t, bus)
	})
}

func testSubscribe(t *testing.T, bus MessageBus) {
	ctx := context.Background()

	channel := newID()
	subA, err := bus.Subscribe(ctx, channel)
	require.NoError(t, err)
	subB, err := bus.Subscribe(ctx, channel)
	require.NoError(t, err)

	require.NoError(t, bus.Publish(ctx, channel, &internal.Request{
		RequestId: "1",
	}))

	msgA := <-subA.Channel()
	msgB := <-subB.Channel()
	require.NotNil(t, msgA)
	require.NotNil(t, msgB)
	require.Equal(t, "1", msgA.(*internal.Request).RequestId)
	require.Equal(t, "1", msgB.(*internal.Request).RequestId)
}

func testSubscribeQueue(t *testing.T, bus MessageBus) {
	ctx := context.Background()

	channel := newID()
	subA, err := bus.SubscribeQueue(ctx, channel)
	require.NoError(t, err)
	subB, err := bus.SubscribeQueue(ctx, channel)
	require.NoError(t, err)

	require.NoError(t, bus.Publish(ctx, channel, &internal.Request{
		RequestId: "2",
	}))

	received := 0
	select {
	case m := <-subA.Channel():
		if m != nil {
			received++
		}
	case <-time.After(DefaultTimeout):
		// continue
	}

	select {
	case m := <-subB.Channel():
		if m != nil {
			received++
		}
	case <-time.After(DefaultTimeout):
		// continue
	}

	require.Equal(t, 1, received)
}
