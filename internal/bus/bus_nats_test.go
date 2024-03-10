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

		cases := []struct {
			minPubMode  int
			subMode     int
			localRouter bool
		}{
			{LegacySubLegacyPub, LegacySubLegacyPub, false},
			{LegacySubLegacyPub, LegacySubCompatiblePub, false},
			{LegacySubLegacyPub, LegacySubLegacyPub, true},
			{LegacySubLegacyPub, LegacySubCompatiblePub, true},
			{LegacySubLegacyPub, RouterSubCompatiblePub, true},
			{LegacySubCompatiblePub, RouterSubWildcardPub, true},
		}

		for _, c := range cases {
			ChannelMode.Store(uint32(c.subMode))

			chA := Channel{
				Legacy: "test|foo|bar",
				Server: "test",
			}
			chB := Channel{
				Legacy: "test|foo|baz",
				Server: "test",
			}
			if c.localRouter {
				chA.Local = "foo.bar"
				chB.Local = "foo.baz"
			}

			subA, err := Subscribe[*internal.Request](ctx, bus, chA, DefaultChannelSize)
			require.NoError(t, err)
			subB, err := Subscribe[*internal.Request](ctx, bus, chB, DefaultChannelSize)
			require.NoError(t, err)

			for pubMode := c.minPubMode; pubMode <= c.subMode; pubMode++ {
				ChannelMode.Store(uint32(pubMode))

				t.Run(fmt.Sprintf("sub:%d/pub:%d/wild:%t", c.subMode, pubMode, c.localRouter), func(t *testing.T) {
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
	})
}
