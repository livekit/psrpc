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

package natsbus_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus"
	"github.com/livekit/psrpc/pkg/bus/natsbus/natstest"
	"github.com/livekit/psrpc/pkg/rand"
)

type delivery struct {
	payload []byte
	subject string
}

// Against a real server: the no-responders status is broker behavior, so there
// is nothing to fake.
func TestInbox(t *testing.T) {
	s := natstest.New(t)
	b, ok := s.Connect(t).(bus.InboxBus)
	require.True(t, ok, "a NATS bus must be an inbox bus")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	inbox, err := b.SubscribeInbox(ctx, bus.Channel{Server: "CLI.test.client.*.RES"}, 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = inbox.Close() })
	deliveries := make(chan delivery, 10)
	go func() {
		for {
			payload, subject, ok := inbox.ReadInbox()
			if !ok {
				return
			}
			deliveries <- delivery{payload, subject}
		}
	}()
	quiet := func(t *testing.T) {
		select {
		case d := <-deliveries:
			t.Fatalf("unexpected delivery on %q", d.subject)
		case <-time.After(100 * time.Millisecond):
		}
	}

	request := bus.Channel{Server: "SRV.test.topic.Q"}
	publish := func(id string) {
		require.NoError(t, b.PublishWithReply(ctx, request,
			bus.Channel{Server: "CLI.test.client." + id + ".RES"}, &internal.Request{RequestId: id}))
	}

	t.Run("no subscriber", func(t *testing.T) {
		publish("a")
		select {
		case d := <-deliveries:
			require.Empty(t, d.payload, "a report has no payload")
			require.Equal(t, "CLI.test.client.a.RES", d.subject, "the report names the request's reply subject")
		case <-ctx.Done():
			t.Fatal("no report")
		}
	})

	// A separate connection, as a server would be.
	sub, err := bus.SubscribeQueue[*internal.Request](ctx, s.Connect(t), request, 10)
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Close() })
	// SUB reaches the server asynchronously; until it does, requests are reported.
	require.Eventually(t, func() bool {
		publish("probe" + rand.NewString())
		select {
		case <-deliveries:
			return false
		case <-sub.Channel():
			return true
		}
	}, 2*time.Second, 10*time.Millisecond)

	t.Run("subscriber", func(t *testing.T) {
		publish("b")
		require.Equal(t, "b", (<-sub.Channel()).RequestId)
		quiet(t)
	})

	t.Run("replies", func(t *testing.T) {
		require.NoError(t, s.Connect(t).Publish(ctx, bus.Channel{Server: "CLI.test.client.b.RES"},
			&internal.Response{RequestId: "b"}))
		select {
		case d := <-deliveries:
			require.NotEmpty(t, d.payload)
			require.Equal(t, "CLI.test.client.b.RES", d.subject)
		case <-ctx.Done():
			t.Fatal("reply not received")
		}
	})
}
