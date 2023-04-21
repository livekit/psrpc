package bus

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/rand"
)

const defaultClientTimeout = time.Second * 3

func TestMessageBus(t *testing.T) {
	t.Run("Local", func(t *testing.T) {
		bus := NewLocalMessageBus()
		testSubscribe(t, bus)
		testSubscribeQueue(t, bus)
		testSubscribeClose(t, bus)
	})

	t.Run("Redis", func(t *testing.T) {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		bus := NewRedisMessageBus(rc)
		testSubscribe(t, bus)
		testSubscribeQueue(t, bus)
		testSubscribeClose(t, bus)
	})

	t.Run("Nats", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		bus := NewNatsMessageBus(nc)
		testSubscribe(t, bus)
		testSubscribeQueue(t, bus)
		testSubscribeClose(t, bus)
	})
}

func testSubscribe(t *testing.T, bus MessageBus) {
	ctx := context.Background()

	channel := rand.String()
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

	channel := rand.String()
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
	case <-time.After(defaultClientTimeout):
		// continue
	}

	select {
	case m := <-subB.Channel():
		if m != nil {
			received++
		}
	case <-time.After(defaultClientTimeout):
		// continue
	}

	require.Equal(t, 1, received)
}

func testSubscribeClose(t *testing.T, bus MessageBus) {
	ctx := context.Background()

	channel := rand.String()
	sub, err := Subscribe[*internal.Request](ctx, bus, channel, DefaultChannelSize)
	require.NoError(t, err)

	require.NoError(t, sub.Close())
	time.Sleep(time.Millisecond * 100)

	select {
	case _, ok := <-sub.Channel():
		require.False(t, ok)
	default:
		require.FailNow(t, "closed subscription channel should not block")
	}
}
