package bus

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestNats(t *testing.T) {
	ctx := context.Background()

	t.Run("wildcard", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		t.Cleanup(nc.Close)
		bus := NewNatsMessageBus(nc)

		for _, wildcard := range []bool{false, true} {
			for subMode := 0; subMode <= finalChannelMode; subMode++ {
				ChannelMode.Store(uint32(subMode))

				chA := Channel{
					Legacy:  "test|foo|bar",
					Primary: "test.foo.bar",
				}
				chB := Channel{
					Legacy:  "test|foo|baz",
					Primary: "test.foo.baz",
				}
				if wildcard {
					chA.Wildcard = "test.*.bar"
					chB.Wildcard = "test.*.baz"
				}

				subA, err := Subscribe[*internal.Request](ctx, bus, chA, DefaultChannelSize)
				require.NoError(t, err)
				subB, err := Subscribe[*internal.Request](ctx, bus, chB, DefaultChannelSize)
				require.NoError(t, err)

				for pubMode := subMode; pubMode <= finalChannelMode; pubMode++ {
					ChannelMode.Store(uint32(pubMode))

					t.Run(fmt.Sprintf("sub:%d/pub:%d/wild:%t", subMode, pubMode, wildcard), func(t *testing.T) {
						require.NoError(t, bus.Publish(ctx, chA, &internal.Request{RequestId: "1"}))

						select {
						case <-subA.Channel():
						case <-time.After(100 * time.Millisecond):
							require.FailNow(t, "expected message from channel A")
						}

						select {
						case <-subB.Channel():
							require.FailNow(t, "expected no message from channel B")
						case <-time.After(100 * time.Millisecond):
						}

						require.NoError(t, bus.Publish(ctx, chB, &internal.Request{RequestId: "1"}))

						select {
						case <-subB.Channel():
						case <-time.After(100 * time.Millisecond):
							require.FailNow(t, "expected message from channel B")
						}

						select {
						case <-subA.Channel():
							require.FailNow(t, "expected no message from channel A")
						case <-time.After(100 * time.Millisecond):
						}
					})
				}

				require.NoError(t, subA.Close())
				require.NoError(t, subB.Close())
			}
		}
	})
}

func BenchmarkNats(b *testing.B) {
	nc, _ := nats.Connect(nats.DefaultURL)
	b.Cleanup(nc.Close)
	bus := NewNatsMessageBus(nc)

	ChannelMode.Store(WildcardSubWildcardPub)

	ctx := context.Background()

	ch := Channel{
		Legacy:   "test|foo|baz",
		Primary:  "test.foo.baz",
		Wildcard: "test.foo.*",
	}
	sub, err := Subscribe[*internal.Request](ctx, bus, ch, DefaultChannelSize)
	require.NoError(b, err)

	n := b.N
	if n == 0 {
		n = 1
	}

	go func() {
		for i := 0; i < n; i++ {
			bus.Publish(ctx, ch, &internal.Request{RequestId: "1"})
		}
		time.AfterFunc(time.Second, func() { sub.Close() })
	}()

	for i := 0; i < n; i++ {
		if _, ok := <-sub.Channel(); !ok {
			return
		}
	}
}
