package psrpc

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lithammer/shortuuid/v3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

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

	serverA, err := NewRPCServer(serviceName, newID(), bus)
	require.NoError(t, err)
	serverB, err := NewRPCServer(serviceName, newID(), bus)
	require.NoError(t, err)
	client, err := NewRPCClient(serviceName, newID(), bus)
	require.NoError(t, err)

	counter := 0
	AddOne := func(ctx context.Context, req proto.Message) (proto.Message, error) {
		counter++
		return &internal.Response{RequestId: req.(*internal.Request).RequestId}, nil
	}

	rpc := "add_one"
	err = serverA.RegisterHandler(rpc, AddOne)
	require.NoError(t, err)
	err = serverB.RegisterHandler(rpc, AddOne)
	require.NoError(t, err)

	requestID := newRequestID()
	res, err := client.SendSingleRequest(context.Background(), rpc, &internal.Request{RequestId: requestID})
	require.NoError(t, err)
	require.Equal(t, 1, counter)
	require.Equal(t, res.(*internal.Response).RequestId, requestID)

	requestID = newRequestID()
	resChan, err := client.SendMultiRequest(context.Background(), rpc, &internal.Request{RequestId: requestID})
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		select {
		case res := <-resChan:
			if res == nil {
				require.Equal(t, 3, counter)
				return
			}
			require.Equal(t, res.(*internal.Response).RequestId, requestID)
		case <-time.After(time.Second * 3):
			t.Fatal("response missing")
		}
	}
}

func newID() string {
	return shortuuid.New()[:12]
}
