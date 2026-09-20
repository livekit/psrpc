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

package bus_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/pkg/bus"
)

// Must stay in the external test package: reaching only the exported API is
// what makes this a proof that a third-party broker can implement Transport.
type fanoutTransport struct {
	mu      sync.Mutex
	readers map[string][]*fanoutReader
}

func newFanoutTransport() *fanoutTransport {
	return &fanoutTransport{readers: map[string][]*fanoutReader{}}
}

func (t *fanoutTransport) Publish(_ context.Context, channel bus.Channel, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, r := range t.readers[channel.Legacy] {
		r.c <- payload
	}
	return nil
}

func (t *fanoutTransport) Subscribe(_ context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := &fanoutReader{c: make(chan []byte, size)}
	t.readers[channel.Legacy] = append(t.readers[channel.Legacy], r)
	return r, nil
}

func (t *fanoutTransport) SubscribeQueue(ctx context.Context, channel bus.Channel, size int) (bus.Reader, error) {
	return t.Subscribe(ctx, channel, size)
}

type fanoutReader struct {
	c         chan []byte
	closeOnce sync.Once
}

func (r *fanoutReader) Read() ([]byte, bool) {
	b, ok := <-r.c
	return b, ok
}

func (r *fanoutReader) Close() error {
	r.closeOnce.Do(func() { close(r.c) })
	return nil
}

func TestThirdPartyTransport(t *testing.T) {
	for _, c := range []struct {
		name string
		opts []bus.BusOption
	}{
		{name: "plain"},
		// Threshold 1 puts every message on the compressed path.
		{name: "gzip", opts: []bus.BusOption{bus.WithBusCompression(bus.CompressionOpts{
			Quality:   6,
			Threshold: 1,
		})}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := bus.New(newFanoutTransport(), c.opts...)

			sub, err := bus.Subscribe[*internal.Request](
				context.Background(), b, bus.Channel{Legacy: "test"}, 1,
			)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, sub.Close()) })

			want := &internal.Request{RequestId: "from a third-party bus"}
			require.NoError(t, b.Publish(context.Background(), bus.Channel{Legacy: "test"}, want))

			select {
			case got := <-sub.Channel():
				require.True(t, proto.Equal(want, got))
			case <-time.After(defaultClientTimeout):
				t.Fatal("timed out waiting for the message")
			}
		})
	}
}

func TestThirdPartyTransportCarriesMaxDecompressedSize(t *testing.T) {
	b := bus.New(newFanoutTransport(), bus.WithBusCompression(bus.CompressionOpts{
		Quality:             6,
		Threshold:           1,
		MaxDecompressedSize: 4096,
	}))
	require.Equal(t, 4096, b.MaxDecompressedSize())
}

// Local is stamped for every transport, not just the ones that route on it.
func TestPublishStampsLocalChannel(t *testing.T) {
	tr := &capturingTransport{}
	b := bus.New(tr)

	want := &internal.Request{RequestId: "req"}
	require.NoError(t, b.Publish(
		context.Background(),
		bus.Channel{Legacy: "legacy", Server: "server", Local: "local"},
		want,
	))

	local, err := bus.DecodeLocalChannel(tr.payload)
	require.NoError(t, err)
	require.Equal(t, "local", local)

	got, err := bus.Deserialize(tr.payload, 0)
	require.NoError(t, err)
	require.True(t, proto.Equal(want, got))
}

type capturingTransport struct {
	bus.Transport
	payload []byte
}

func (t *capturingTransport) Publish(_ context.Context, _ bus.Channel, payload []byte) error {
	t.payload = payload
	return nil
}
