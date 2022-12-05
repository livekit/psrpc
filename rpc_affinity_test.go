package psrpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestAffinity(t *testing.T) {
	testAffinity(t, AffinityOpts{
		AcceptFirstAvailable: true,
	}, "1")

	testAffinity(t, AffinityOpts{
		AcceptFirstAvailable: true,
		MinimumAffinity:      0.5,
	}, "2")

	testAffinity(t, AffinityOpts{
		ShortCircuitTimeout: time.Millisecond * 150,
	}, "2")

	testAffinity(t, AffinityOpts{
		MinimumAffinity:     0.4,
		AffinityTimeout:     0,
		ShortCircuitTimeout: time.Millisecond * 250,
	}, "3")

	testAffinity(t, AffinityOpts{
		MinimumAffinity:     0.3,
		AffinityTimeout:     time.Millisecond * 250,
		ShortCircuitTimeout: time.Millisecond * 200,
	}, "2")

	testAffinity(t, AffinityOpts{
		AffinityTimeout: time.Millisecond * 600,
	}, "5")
}

func testAffinity(t *testing.T, opts AffinityOpts, expectedID string) {
	c := make(chan *internal.ClaimRequest, 100)
	go func() {
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "1",
			Affinity:  0.1,
		}
		time.Sleep(time.Millisecond * 100)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "2",
			Affinity:  0.5,
		}
		time.Sleep(time.Millisecond * 200)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "3",
			Affinity:  0.7,
		}
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "4",
			Affinity:  0.1,
		}
		time.Sleep(time.Millisecond * 200)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "5",
			Affinity:  0.9,
		}
	}()
	serverID, err := selectServer(context.Background(), c, opts)
	require.NoError(t, err)
	require.Equal(t, expectedID, serverID)
}
