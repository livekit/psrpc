// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package my_service

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/bus/bustest"
)

func TestGeneratedService(t *testing.T) {
	bustest.TestAll(t, testGeneratedService)
}

func testGeneratedService(t *testing.T, bus func(t testing.TB) bus.MessageBus) {
	ctx := context.Background()
	req := &MyRequest{}
	update := &MyUpdate{}
	sA := createServer(t, bus(t))
	sB := createServer(t, bus(t))

	t.Cleanup(func() {
		shutdown(t, sA)
		shutdown(t, sB)
	})

	requestCount := 0
	requestHook := func(ctx context.Context, req proto.Message, rpcInfo psrpc.RPCInfo) {
		requestCount++
	}
	responseCount := 0
	responseHook := func(ctx context.Context, req proto.Message, rpcInfo psrpc.RPCInfo, res proto.Message, err error) {
		responseCount++
	}
	cA := createClient(t, bus(t), psrpc.WithClientRequestHooks(requestHook), psrpc.WithClientResponseHooks(responseHook))
	cB := createClient(t, bus(t))

	// rpc NormalRPC(MyRequest) returns (MyResponse);
	_, err := cA.NormalRPC(ctx, req)
	require.NoError(t, err)

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["NormalRPC"]+sB.counts["NormalRPC"])
	sA.Unlock()
	sB.Unlock()
	require.Equal(t, 1, requestCount)
	require.Equal(t, 1, responseCount)

	// rpc IntensiveRPC(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).type = AFFINITY;
	_, err = cB.IntensiveRPC(ctx, req, psrpc.WithSelectionOpts(psrpc.SelectionOpts{
		// if using AcceptFirstAvailable, local bus can fail the affinity count check by processing too quickly
		AffinityTimeout: time.Millisecond * 250,
	}))
	require.NoError(t, err)

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["IntensiveRPC"]+sB.counts["IntensiveRPC"])
	require.Equal(t, 1, sA.counts["IntensiveRPCAffinity"])
	require.Equal(t, 1, sB.counts["IntensiveRPCAffinity"])
	sA.Unlock()
	sB.Unlock()

	// rpc GetStats(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).type = MULTI;
	respChan, err := cA.GetStats(ctx, req)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		select {
		case res := <-respChan:
			require.NotNil(t, res)
			require.NoError(t, res.Err)
		case <-time.After(time.Second * 3):
			require.FailNow(t, "timed out")
		}
	}

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["GetStats"])
	require.Equal(t, 1, sB.counts["GetStats"])
	sA.Unlock()
	sB.Unlock()
	require.Equal(t, 2, requestCount)
	require.Equal(t, 3, responseCount)

	stream, err := cA.ExchangeUpdates(ctx)
	require.NoError(t, err)
	require.NoError(t, stream.Send(&MyClientMessage{}))
	require.NoError(t, stream.Send(&MyClientMessage{}))
	require.NoError(t, stream.Close(nil))

	// let the service goroutine run
	time.Sleep(time.Second)

	sA.Lock()
	sB.Lock()
	require.Condition(t, func() bool {
		return sA.counts["ExchangeUpdates"] == 2 || sB.counts["ExchangeUpdates"] == 2
	})
	sA.Unlock()
	sB.Unlock()

	// rpc GetRegionStats(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).topics = true;
	//   option (psrpc.options).type = MULTI;
	require.NoError(t, sA.server.RegisterGetRegionStatsTopic("regionA"))
	require.NoError(t, sA.server.RegisterGetRegionStatsTopic("regionB"))
	sA.server.DeregisterGetRegionStatsTopic("regionB")
	require.NoError(t, sB.server.RegisterGetRegionStatsTopic("regionB"))
	time.Sleep(time.Second)

	respChan, err = cB.GetRegionStats(ctx, "regionB", req)
	require.NoError(t, err)
	select {
	case res := <-respChan:
		require.NotNil(t, res)
		require.NoError(t, res.Err)
	case <-time.After(time.Second):
		require.FailNow(t, "timed out")
	}

	sA.Lock()
	sB.Lock()
	require.Equal(t, 0, sA.counts["GetRegionStats"])
	require.Equal(t, 1, sB.counts["GetRegionStats"])
	sA.Unlock()
	sB.Unlock()

	// rpc UpdateRegionState(Ignored) returns (MyUpdate) {
	//   option (psrpc.options).subscription = true;
	//   option (psrpc.options).topics = true;
	//   option (psrpc.options).type = MULTI;
	subA, err := cA.SubscribeUpdateRegionState(ctx, "regionA")
	require.NoError(t, err)
	subB, err := cB.SubscribeUpdateRegionState(ctx, "regionA")
	require.NoError(t, err)
	time.Sleep(time.Second)

	require.NoError(t, sB.server.PublishUpdateRegionState(ctx, "regionA", update))
	requireTwo(t, subA, subB)
	require.NoError(t, subA.Close())
	require.NoError(t, subB.Close())
}

func requireTwo(t *testing.T, subA, subB psrpc.Subscription[*MyUpdate]) {
	for i := 0; i < 2; i++ {
		select {
		case <-subA.Channel():
		case <-subB.Channel():
		case <-time.After(time.Second):
			require.FailNow(t, "timed out")
		}
	}
}

func createServer(t *testing.T, bus psrpc.MessageBus) *MyService {
	svc := &MyService{
		counts: make(map[string]int),
	}
	server, err := NewMyServiceServer(svc, bus)
	require.NoError(t, err)
	svc.server = server
	return svc
}

func createClient(t *testing.T, bus psrpc.MessageBus, opts ...psrpc.ClientOption) MyServiceClient {
	client, err := NewMyServiceClient(bus, opts...)
	require.NoError(t, err)
	return client
}

func shutdown(t *testing.T, s *MyService) {
	done := make(chan struct{})
	go func() {
		s.server.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	// continue
	case <-time.After(time.Second * 3):
		require.FailNow(t, "shutdown not returning")
	}
}

type MyService struct {
	sync.Mutex

	server MyServiceServer
	counts map[string]int
}

func (s *MyService) NormalRPC(_ context.Context, _ *MyRequest) (*MyResponse, error) {
	s.Lock()
	s.counts["NormalRPC"]++
	s.Unlock()
	return &MyResponse{}, nil
}

func (s *MyService) IntensiveRPC(_ context.Context, _ *MyRequest) (*MyResponse, error) {
	s.Lock()
	s.counts["IntensiveRPC"]++
	s.Unlock()
	return &MyResponse{}, nil
}

func (s *MyService) IntensiveRPCAffinity(_ context.Context, _ *MyRequest) float32 {
	s.Lock()
	s.counts["IntensiveRPCAffinity"]++
	s.Unlock()
	return rand.Float32()
}

func (s *MyService) GetStats(_ context.Context, _ *MyRequest) (*MyResponse, error) {
	s.Lock()
	s.counts["GetStats"]++
	s.Unlock()
	return &MyResponse{}, nil
}

func (s *MyService) ExchangeUpdates(stream psrpc.ServerStream[*MyServerMessage, *MyClientMessage]) error {
	for range stream.Channel() {
		s.Lock()
		s.counts["ExchangeUpdates"]++
		s.Unlock()
	}
	return nil
}

func (s *MyService) GetRegionStats(_ context.Context, _ *MyRequest) (*MyResponse, error) {
	s.Lock()
	s.counts["GetRegionStats"]++
	s.Unlock()
	return &MyResponse{}, nil
}
