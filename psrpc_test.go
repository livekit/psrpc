package psrpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lithammer/shortuuid/v3"
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

func TestRPC(t *testing.T) {
	cases := []struct {
		label string
		bus   func() MessageBus
	}{
		{
			label: "Local",
			bus:   func() MessageBus { return NewLocalMessageBus() },
		},
		{
			label: "Redis",
			bus: func() MessageBus {
				rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
				return NewRedisMessageBus(rc)
			},
		},
		{
			label: "Nats",
			bus: func() MessageBus {
				nc, _ := nats.Connect(nats.DefaultURL)
				return NewNatsMessageBus(nc)
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

func testRPC(t *testing.T, bus MessageBus) {
	serviceName := "test"

	serverA := NewRPCServer(serviceName, newID(), bus)
	serverB := NewRPCServer(serviceName, newID(), bus)
	serverC := NewRPCServer(serviceName, newID(), bus)

	t.Cleanup(func() {
		serverA.Close(true)
		serverB.Close(true)
		serverC.Close(true)
	})

	client, err := NewRPCClient(serviceName, newID(), bus)
	require.NoError(t, err)

	retErr := NewErrorf(Internal, "foo")

	counter := 0
	errCount := 0
	rpc := "add_one"
	multi_rpc := "add_one_multi"
	addOne := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		counter++
		return &internal.Response{RequestId: req.RequestId}, nil
	}
	returnError := func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
		return nil, retErr
	}
	err = RegisterHandler[*internal.Request, *internal.Response](serverA, rpc, nil, addOne, nil, true, false)
	require.NoError(t, err)
	err = RegisterHandler[*internal.Request, *internal.Response](serverB, rpc, nil, addOne, nil, true, false)
	require.NoError(t, err)

	ctx := context.Background()
	requestID := newRequestID()
	res, err := RequestSingle[*internal.Response](
		ctx, client, rpc, nil, true, &internal.Request{RequestId: requestID},
	)

	require.NoError(t, err)
	require.Equal(t, 1, counter)
	require.Equal(t, res.RequestId, requestID)

	err = RegisterHandler[*internal.Request, *internal.Response](serverA, multi_rpc, nil, addOne, nil, false, true)
	require.NoError(t, err)
	err = RegisterHandler[*internal.Request, *internal.Response](serverB, multi_rpc, nil, addOne, nil, false, true)
	require.NoError(t, err)
	err = RegisterHandler[*internal.Request, *internal.Response](serverC, multi_rpc, nil, returnError, nil, false, true)
	require.NoError(t, err)

	requestID = newRequestID()
	resChan, err := RequestMulti[*internal.Response](
		ctx, client, multi_rpc, nil, &internal.Request{RequestId: requestID},
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
		case <-time.After(DefaultClientTimeout + time.Second):
			t.Fatal("response missing")
		}
	}
}

func testStream(t *testing.T, bus MessageBus) {
	serviceName := "test_stream"

	serverA := NewRPCServer(serviceName, newID(), bus)

	t.Cleanup(func() {
		serverA.Close(true)
	})

	client, err := NewRPCClientWithStreams(serviceName, newID(), bus)
	require.NoError(t, err)

	serverClose := make(chan struct{})
	rpc := "ping_pong"
	handlePing := func(stream ServerStream[*internal.Response, *internal.Response]) error {
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
	err = RegisterStreamHandler[*internal.Response, *internal.Response](serverA, rpc, nil, handlePing, nil, true)
	require.NoError(t, err)

	ctx := context.Background()
	stream, err := OpenStream[*internal.Response, *internal.Response](
		ctx, client, rpc, nil, true,
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
		case <-time.After(DefaultClientTimeout):
			t.Fatal("no pong received")
		}
	}

	assert.NoError(t, stream.Close(nil))

	select {
	case <-serverClose:
	case <-time.After(DefaultClientTimeout):
		t.Fatal("server did not close")
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
	serverID, err := selectServer(context.Background(), c, nil, opts)
	require.NoError(t, err)
	require.Equal(t, expectedID, serverID)
}

func newID() string {
	return shortuuid.New()[:12]
}
