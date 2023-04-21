package test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/rand"
	client2 "github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/server"
)

func TestRPC(t *testing.T) {
	cases := []struct {
		label string
		bus   func() psrpc.MessageBus
	}{
		{
			label: "Local",
			bus:   func() psrpc.MessageBus { return psrpc.NewLocalMessageBus() },
		},
		{
			label: "Redis",
			bus: func() psrpc.MessageBus {
				rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
				return psrpc.NewRedisMessageBus(rc)
			},
		},
		{
			label: "Nats",
			bus: func() psrpc.MessageBus {
				nc, _ := nats.Connect(nats.DefaultURL)
				return psrpc.NewNatsMessageBus(nc)
			},
		},
	}

	for _, c := range cases {
		c := c
		t.Run(fmt.Sprintf("RPC/%s", c.label), func(t *testing.T) {
			testRPC(t, c.bus())
		})
		t.Run(fmt.Sprintf("Stream/%s", c.label), func(t *testing.T) {
			testStream(t, c.bus())
		})
	}
}

func testRPC(t *testing.T, bus psrpc.MessageBus) {
	serviceName := "test"

	serverA := server.NewRPCServer(serviceName, rand.String(), bus)
	serverB := server.NewRPCServer(serviceName, rand.String(), bus)
	serverC := server.NewRPCServer(serviceName, rand.String(), bus)

	t.Cleanup(func() {
		serverA.Close(true)
		serverB.Close(true)
		serverC.Close(true)
	})

	c, err := client2.NewRPCClient(serviceName, rand.String(), bus)
	require.NoError(t, err)

	retErr := psrpc.NewErrorf(psrpc.Internal, "foo")

	counter := 0
	errCount := 0
	rpc := "add_one"
	multiRpc := "add_one_multi"
	addOne := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		counter++
		return &internal.Response{RequestId: req.RequestId}, nil
	}
	returnError := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		return nil, retErr
	}
	err = server.RegisterHandler[*internal.Request, *internal.Response](serverA, rpc, nil, addOne, nil, true, false)
	require.NoError(t, err)
	err = server.RegisterHandler[*internal.Request, *internal.Response](serverB, rpc, nil, addOne, nil, true, false)
	require.NoError(t, err)

	ctx := context.Background()
	requestID := rand.NewRequestID()
	res, err := client2.RequestSingle[*internal.Response](
		ctx, c, rpc, nil, true, &internal.Request{RequestId: requestID},
	)

	require.NoError(t, err)
	require.Equal(t, 1, counter)
	require.Equal(t, res.RequestId, requestID)

	err = server.RegisterHandler[*internal.Request, *internal.Response](serverA, multiRpc, nil, addOne, nil, false, true)
	require.NoError(t, err)
	err = server.RegisterHandler[*internal.Request, *internal.Response](serverB, multiRpc, nil, addOne, nil, false, true)
	require.NoError(t, err)
	err = server.RegisterHandler[*internal.Request, *internal.Response](serverC, multiRpc, nil, returnError, nil, false, true)
	require.NoError(t, err)

	requestID = rand.NewRequestID()
	resChan, err := client2.RequestMulti[*internal.Response](
		ctx, c, multiRpc, nil, &internal.Request{RequestId: requestID},
	)
	require.NoError(t, err)

	for i := 0; i < 4; i++ {
		select {
		case res := <-resChan:
			if res == nil {
				require.Equal(t, 3, counter)
				require.Equal(t, 1, errCount)
				return
			}
			if res.Err != nil {
				errCount++
				require.Equal(t, retErr, res.Err)
			} else {
				require.Equal(t, res.Result.RequestId, requestID)
			}
		case <-time.After(psrpc.DefaultClientTimeout + time.Second):
			t.Fatal("response missing")
		}
	}
}

func testStream(t *testing.T, bus psrpc.MessageBus) {
	serviceName := "test_stream"

	serverA := server.NewRPCServer(serviceName, rand.String(), bus)

	t.Cleanup(func() {
		serverA.Close(true)
	})

	c, err := client2.NewRPCClientWithStreams(serviceName, rand.String(), bus)
	require.NoError(t, err)

	serverClose := make(chan struct{})
	rpc := "ping_pong"
	handlePing := func(stream psrpc.ServerStream[*internal.Response, *internal.Response]) error {
		defer close(serverClose)

		for ping := range stream.Channel() {
			pong := &internal.Response{
				SentAt: ping.SentAt,
				Code:   "PONG",
			}
			err := stream.Send(pong)
			require.NoError(t, err)
		}
		return nil
	}
	err = server.RegisterStreamHandler[*internal.Response, *internal.Response](serverA, rpc, nil, handlePing, nil, true)
	require.NoError(t, err)

	ctx := context.Background()
	stream, err := client2.OpenStream[*internal.Response, *internal.Response](
		ctx, c, rpc, nil, true,
	)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		err = stream.Send(&internal.Response{
			Code: "PING",
		})
		require.NoError(t, err)

		select {
		case pong := <-stream.Channel():
			require.Equal(t, "PONG", pong.Code)
		case <-time.After(psrpc.DefaultClientTimeout):
			t.Fatal("no pong received")
		}
	}

	assert.NoError(t, stream.Close(nil))

	select {
	case <-serverClose:
	case <-time.After(psrpc.DefaultClientTimeout):
		t.Fatal("server did not close")
	}
}
