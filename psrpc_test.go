package psrpc

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lithammer/shortuuid/v3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestRPC(t *testing.T) {
	t.Run("Local", func(t *testing.T) {
		testRPC(t, NewLocalMessageBus())
	})

	t.Run("Redis", func(t *testing.T) {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		testRPC(t, NewRedisMessageBus(rc))
	})

	t.Run("Nats", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		testRPC(t, NewNatsMessageBus(nc))
	})
}

func testRPC(t *testing.T, bus MessageBus) {
	serviceName := "test"

	serverA := NewRPCServer(serviceName, newID(), bus)
	serverB := NewRPCServer(serviceName, newID(), bus)
	client, err := NewRPCClient(serviceName, newID(), bus)
	require.NoError(t, err)

	counter := 0
	rpc := "add_one"
	addOne := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		counter++
		return &internal.Response{RequestId: req.RequestId}, nil
	}
	err = RegisterHandler[*internal.Request, *internal.Response](serverA, rpc, "", addOne, nil)
	require.NoError(t, err)
	err = RegisterHandler[*internal.Request, *internal.Response](serverB, rpc, "", addOne, nil)
	require.NoError(t, err)

	ctx := context.Background()
	requestID := newRequestID()
	res, err := RequestSingle[*internal.Response](
		ctx, client, rpc, "", &internal.Request{RequestId: requestID},
	)

	require.NoError(t, err)
	require.Equal(t, 1, counter)
	require.Equal(t, res.RequestId, requestID)

	requestID = newRequestID()
	resChan, err := RequestMulti[*internal.Response](
		ctx, client, rpc, "", &internal.Request{RequestId: requestID},
	)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		select {
		case res := <-resChan:
			if res == nil {
				require.Equal(t, 3, counter)
				return
			}
			require.Equal(t, res.Result.RequestId, requestID)
		case <-time.After(DefaultClientTimeout + time.Second):
			t.Fatal("response missing")
		}
	}
}

func TestAffinity(t *testing.T) {
	testAffinity(t, SelectionOpts{
		AcceptFirstAvailable: true,
	}, "1")

	testAffinity(t, SelectionOpts{
		AcceptFirstAvailable: true,
		MinimumAffinity:      0.5,
	}, "2")

	testAffinity(t, SelectionOpts{
		ShortCircuitTimeout: time.Millisecond * 150,
	}, "2")

	testAffinity(t, SelectionOpts{
		MinimumAffinity:     0.4,
		AffinityTimeout:     0,
		ShortCircuitTimeout: time.Millisecond * 250,
	}, "3")

	testAffinity(t, SelectionOpts{
		MinimumAffinity:     0.3,
		AffinityTimeout:     time.Millisecond * 250,
		ShortCircuitTimeout: time.Millisecond * 200,
	}, "2")

	testAffinity(t, SelectionOpts{
		AffinityTimeout: time.Millisecond * 600,
	}, "5")
}

func testAffinity(t *testing.T, opts SelectionOpts, expectedID string) {
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

func newID() string {
	return shortuuid.New()[:12]
}
