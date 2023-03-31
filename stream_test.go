package psrpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.uber.org/atomic"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
)

type testStreamAdapter struct {
	sendCalls  atomic.Int32
	closeCalls atomic.Int32
}

func (a *testStreamAdapter) send(ctx context.Context, msg *internal.Stream) error {
	a.sendCalls.Inc()
	return nil
}

func (a *testStreamAdapter) close(streamID string) {
	a.closeCalls.Inc()
}

func newTestStream() *streamImpl[*internal.Request, *internal.Response] {
	stream := &streamImpl[*internal.Request, *internal.Response]{
		streamOpts: streamOpts{
			timeout: time.Second,
		},
		adapter:  &testStreamAdapter{},
		streamID: newStreamID(),
		acks:     map[string]chan struct{}{},
		recvChan: make(chan *internal.Response),
	}
	stream.ctx, stream.cancelCtx = context.WithCancel(context.Background())
	stream.interceptor = &streamInterceptorRoot[*internal.Request, *internal.Response]{stream}
	return stream
}

func TestStream(t *testing.T) {
	t.Run("remotely closed streams yield stream closed error", func(t *testing.T) {
		stream := newTestStream()
		var err atomic.Value

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			err.Store(stream.Send(&internal.Request{}))
			wg.Done()
		}()
		go func() {
			for stream.pending.Load() == 0 {
				time.Sleep(time.Millisecond)
			}
			stream.handleStream(&internal.Stream{
				Body: &internal.Stream_Close{
					Close: &internal.StreamClose{
						Error: ErrStreamClosed.Error(),
						Code:  string(ErrStreamClosed.Code()),
					},
				},
			})
			wg.Done()
		}()
		wg.Wait()

		require.EqualValues(t, ErrStreamClosed, err.Load())
	})
}
