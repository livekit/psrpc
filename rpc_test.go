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
	t.Run("Redis", func(t *testing.T) {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		testRPCs(t, NewRedisMessageBus(rc))
	})

	t.Run("Nats", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		testRPCs(t, NewNatsMessageBus(nc))
	})
}

func testRPCs(t *testing.T, bus MessageBus) {
	serviceName := "test"

	serverA := NewRPCServer(serviceName, newID(), bus)
	serverB := NewRPCServer(serviceName, newID(), bus)
	client, err := NewRPCClient(serviceName, newID(), bus)
	require.NoError(t, err)

	counter := 0
	rpc := "add_one"
	addOneHandler := NewHandler(rpc, func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		counter++
		return &internal.Response{RequestId: req.RequestId}, nil
	})
	err = serverA.RegisterHandler(addOneHandler)
	require.NoError(t, err)
	err = serverB.RegisterHandler(addOneHandler)
	require.NoError(t, err)

	ctx := context.Background()
	requestID := newRequestID()
	res, err := RequestSingle[*internal.Response](
		ctx,
		client,
		rpc,
		&internal.Request{RequestId: requestID})

	require.NoError(t, err)
	require.Equal(t, 1, counter)
	require.Equal(t, res.RequestId, requestID)

	requestID = newRequestID()
	resChan, err := RequestAll[*internal.Response](
		ctx,
		client,
		rpc,
		&internal.Request{RequestId: requestID})
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		select {
		case res := <-resChan:
			if res == nil {
				require.Equal(t, 3, counter)
				return
			}
			require.Equal(t, res.Result.RequestId, requestID)
		case <-time.After(DefaultTimeout):
			t.Fatal("response missing")
		}
	}
}

func newID() string {
	return shortuuid.New()[:12]
}
