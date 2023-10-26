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

package stream

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
)

func TestClosePendingSend(t *testing.T) {
	s := NewStream[*internal.Request, *internal.Response](
		context.Background(),
		&info.RequestInfo{},
		rand.NewStreamID(),
		psrpc.DefaultClientTimeout,
		&testStreamAdapter{},
		nil,
		make(chan *internal.Response),
		make(map[string]chan struct{}),
	)

	var err atomic.Value
	ready := make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		close(ready)
		err.Store(s.Send(&internal.Request{}))
		wg.Done()
	}()

	var handleErr error
	go func() {
		<-ready
		handleErr = s.HandleStream(&internal.Stream{
			Body: &internal.Stream_Close{
				Close: &internal.StreamClose{
					Error: psrpc.ErrStreamClosed.Error(),
					Code:  string(psrpc.ErrStreamClosed.Code()),
				},
			},
		})
		wg.Done()
	}()
	wg.Wait()

	require.NoError(t, handleErr)
	require.EqualValues(t, psrpc.ErrStreamClosed, err.Load())
}

type testStreamAdapter struct {
	sendCalls  atomic.Int32
	closeCalls atomic.Int32
}

func (a *testStreamAdapter) Send(ctx context.Context, msg *internal.Stream) error {
	a.sendCalls.Inc()
	return nil
}

func (a *testStreamAdapter) Close(streamID string) {
	a.closeCalls.Inc()
}

func TestClosePanic(t *testing.T) {
	for i := 0; i < 1000; i++ {
		s := NewStream[*internal.Request, *internal.Response](
			context.Background(),
			&info.RequestInfo{},
			rand.NewStreamID(),
			psrpc.DefaultClientTimeout,
			&testStreamAdapter{},
			nil,
			make(chan *internal.Response, 1),
			make(map[string]chan struct{}),
		)

		ready := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(2)

		go func() {
			close(ready)
			s.Close(nil)
			wg.Done()
		}()

		go func() {
			b, _ := proto.Marshal(&internal.Response{})

			<-ready
			s.HandleStream(&internal.Stream{
				Body: &internal.Stream_Message{
					Message: &internal.StreamMessage{
						RawMessage: b,
					},
				},
			})
			wg.Done()
		}()

		wg.Wait()
	}
}
