// Copyright 2026 LiveKit, Inc.
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

package client

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/info"
)

func TestOpenStreamRejectsClosedClient(t *testing.T) {
	sd := &info.ServiceDefinition{Name: "test", ID: "client"}
	sd.RegisterMethod("stream", false, false, false, false)
	c, err := NewRPCClientWithStreams(sd, bus.NewLocalMessageBus())
	require.NoError(t, err)
	c.Close()

	_, err = OpenStream[*internal.Request, *internal.Response](
		context.Background(), c, "stream", nil,
	)
	require.ErrorIs(t, err, psrpc.ErrClientClosed)
}

func TestOpenStreamStopsWhileWaitingForAckWhenClientCloses(t *testing.T) {
	openPublished := make(chan struct{})
	b := bus.NewTestBus(bus.NewLocalMessageBus(), func(o *bus.TestBusOpts) {
		o.PublishInterceptors = append(o.PublishInterceptors, func(next bus.PublishHandler) bus.PublishHandler {
			return func(ctx context.Context, channel bus.Channel, msg proto.Message) error {
				err := next(ctx, channel, msg)
				if streamMsg, ok := msg.(*internal.Stream); ok {
					if _, ok := streamMsg.Body.(*internal.Stream_Open); ok {
						close(openPublished)
					}
				}
				return err
			}
		})
	})

	sd := &info.ServiceDefinition{Name: "test", ID: "client"}
	sd.RegisterMethod("stream", false, false, false, false)
	c, err := NewRPCClientWithStreams(sd, b)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := make(chan error, 1)
	go func() {
		_, err := OpenStream[*internal.Request, *internal.Response](
			ctx, c, "stream", nil, psrpc.WithRequestTimeout(time.Minute),
		)
		errChan <- err
	}()

	select {
	case <-openPublished:
	case <-time.After(time.Second):
		t.Fatal("stream open request was not published")
	}
	c.Close()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, psrpc.ErrClientClosed)
	case <-time.After(time.Second):
		t.Fatal("stream open did not stop when the client closed")
	}
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.claimRequests) == 0 && len(c.streamChannels) == 0
	}, time.Second, time.Millisecond)
}

func TestRequestSingleStopsWhenClientCloses(t *testing.T) {
	sd := &info.ServiceDefinition{Name: "test", ID: "client"}
	sd.RegisterMethod("rpc", false, false, false, false)
	c, err := NewRPCClient(sd, bus.NewLocalMessageBus())
	require.NoError(t, err)

	errChan := make(chan error, 1)
	go func() {
		_, err := RequestSingle[*internal.Response](
			context.Background(), c, "rpc", nil, &internal.Request{},
			psrpc.WithRequestTimeout(time.Minute),
		)
		errChan <- err
	}()

	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.responseChannels) == 1
	}, time.Second, time.Millisecond)
	c.Close()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, psrpc.ErrClientClosed)
	case <-time.After(time.Second):
		t.Fatal("request did not stop when the client closed")
	}
}

func TestRequestSingleStopsDuringServerSelectionWhenClientCloses(t *testing.T) {
	sd := &info.ServiceDefinition{Name: "test", ID: "client"}
	sd.RegisterMethod("rpc", true, false, true, false)
	c, err := NewRPCClient(
		sd,
		bus.NewLocalMessageBus(),
		psrpc.WithClientSelectTimeout(time.Minute),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errChan := make(chan error, 1)
	go func() {
		_, err := RequestSingle[*internal.Response](
			ctx, c, "rpc", nil, &internal.Request{},
			psrpc.WithRequestTimeout(time.Minute),
		)
		errChan <- err
	}()

	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.claimRequests) == 1 && len(c.responseChannels) == 1
	}, time.Second, time.Millisecond)
	c.Close()

	select {
	case err := <-errChan:
		require.ErrorIs(t, err, psrpc.ErrClientClosed)
	case <-time.After(time.Second):
		t.Fatal("server selection did not stop when the client closed")
	}
	require.Eventually(t, func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return len(c.claimRequests) == 0 && len(c.responseChannels) == 0
	}, time.Second, time.Millisecond)
}

func TestRequestMultiStopsWhenClientCloses(t *testing.T) {
	sd := &info.ServiceDefinition{Name: "test", ID: "client"}
	sd.RegisterMethod("multi", false, true, false, false)
	c, err := NewRPCClient(sd, bus.NewLocalMessageBus())
	require.NoError(t, err)

	responses, err := RequestMulti[*internal.Response](
		context.Background(), c, "multi", nil, &internal.Request{},
		psrpc.WithRequestTimeout(time.Minute),
	)
	require.NoError(t, err)
	c.Close()

	select {
	case _, ok := <-responses:
		require.False(t, ok)
	case <-time.After(time.Second):
		t.Fatal("multi response channel did not close with the client")
	}
}

func TestBlockedMultiResponseStopsWhenClientCloses(t *testing.T) {
	const requestID = "blocked-response"
	responseStarted := make(chan struct{})
	internalResponses := make(chan *internal.Response, 1)
	c := &RPCClient{
		ClientOpts: psrpc.ClientOpts{
			ResponseHooks: []psrpc.ClientResponseHook{
				func(context.Context, proto.Message, psrpc.RPCInfo, proto.Message, error) {
					close(responseStarted)
				},
			},
		},
		responseChannels: map[string]chan *internal.Response{requestID: internalResponses},
	}
	output := make(chan *psrpc.Response[*internal.Response])
	m := &multiRPC[*internal.Response]{
		c:         c,
		i:         &info.RequestInfo{},
		requestID: requestID,
		resChan:   output,
	}
	m.handler = m

	raw, err := bus.SerializePayload(&internal.Response{})
	require.NoError(t, err)
	internalResponses <- &internal.Response{RawResponse: raw}

	go m.handleResponses(
		context.Background(), &internal.Request{}, internalResponses,
		psrpc.RequestOpts{Timeout: time.Minute},
	)
	<-responseStarted
	c.Close()

	if waitForResponseRouteRemoval(c, requestID, time.Second) {
		return
	}

	// Release the old blocking send before failing so the test leaves no goroutine behind.
	<-output
	require.Eventually(t, func() bool {
		return waitForResponseRouteRemoval(c, requestID, 0)
	}, time.Second, time.Millisecond)
	t.Fatal("blocked multi-RPC response did not stop when the client closed")
}

func waitForResponseRouteRemoval(c *RPCClient, requestID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		_, ok := c.responseChannels[requestID]
		c.mu.Unlock()
		if !ok {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}
