package psrpc

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
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
	subA, err := Subscribe[*internal.Request](ctx, bus, channel, DefaultChannelSize)
	require.NoError(t, err)
	subB, err := Subscribe[*internal.Request](ctx, bus, channel, DefaultChannelSize)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	require.NoError(t, bus.Publish(ctx, channel, &internal.Request{
		RequestId: "1",
	}))

	msgA := <-subA.Channel()
	msgB := <-subB.Channel()
	require.NotNil(t, msgA)
	require.NotNil(t, msgB)
	require.Equal(t, "1", msgA.RequestId)
	require.Equal(t, "1", msgB.RequestId)
}

func testSubscribeQueue(t *testing.T, bus MessageBus) {
	ctx := context.Background()

	channel := newID()
	subA, err := SubscribeQueue[*internal.Request](ctx, bus, channel, DefaultChannelSize)
	require.NoError(t, err)
	subB, err := SubscribeQueue[*internal.Request](ctx, bus, channel, DefaultChannelSize)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	require.NoError(t, bus.Publish(ctx, channel, &internal.Request{
		RequestId: "2",
	}))

	received := 0
	select {
	case m := <-subA.Channel():
		if m != nil {
			received++
		}
	case <-time.After(DefaultClientTimeout):
		// continue
	}

	select {
	case m := <-subB.Channel():
		if m != nil {
			received++
		}
	case <-time.After(DefaultClientTimeout):
		// continue
	}

	require.Equal(t, 1, received)
}
