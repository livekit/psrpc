package test

import (
	"context"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/bus/bustest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

// TestRPCBench measures real PSRPC round-trip performance (request ->
// handler -> response) for every registered bus: Local, Redis, NATS (via
// local Docker) and RabbitMQ (via AMQP_URL, skipped when unset). Run with
// -v to see the per-bus results.
func TestRPCBench(t *testing.T) {
	bustest.TestAll(t, benchRPC)
}

func benchRPC(t *testing.T, busFunc func(t testing.TB) bus.MessageBus) {
	const (
		seqN    = 500
		concW   = 8
		concDur = 10 * time.Second
	)

	serviceName := "bench_" + rand.NewString()
	rpc := "echo"

	// Server and client sit on separate bus instances, mirroring separate
	// processes talking over the same broker.
	srvBus := busFunc(t)
	cliBus := busFunc(t)

	srv := server.NewRPCServer(&info.ServiceDefinition{Name: serviceName, ID: rand.NewString()}, srvBus)
	t.Cleanup(func() { srv.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: serviceName, ID: rand.NewString()}, cliBus)
	require.NoError(t, err)

	srv.RegisterMethod(rpc, false, false, true, false)
	c.RegisterMethod(rpc, false, false, true, false)
	err = server.RegisterHandler[*internal.Request, *internal.Response](srv, rpc, nil,
		func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
			return &internal.Response{RequestId: req.RequestId}, nil
		}, nil)
	require.NoError(t, err)
	time.Sleep(time.Second) // let subscriptions propagate

	ctx := context.Background()

	// Warmup.
	for i := 0; i < 20; i++ {
		_, err = client.RequestSingle[*internal.Response](ctx, c, rpc, nil, &internal.Request{RequestId: rand.NewRequestID()})
		require.NoError(t, err)
	}

	// Sequential round-trip latency.
	seqLats := make([]time.Duration, 0, seqN)
	for i := 0; i < seqN; i++ {
		start := time.Now()
		res, err := client.RequestSingle[*internal.Response](ctx, c, rpc, nil, &internal.Request{RequestId: rand.NewRequestID()})
		require.NoError(t, err)
		require.NotNil(t, res)
		seqLats = append(seqLats, time.Since(start))
	}
	p50, p95, p99, mx := benchPercentiles(seqLats)
	t.Logf("[seq ] n=%d                p50=%-10v p95=%-10v p99=%-10v max=%v",
		seqN, p50, p95, p99, mx)

	// Sustained throughput at fixed concurrency.
	var count, errs atomic.Int64
	var mu sync.Mutex
	var concLats []time.Duration
	deadline := time.Now().Add(concDur)
	var wg sync.WaitGroup
	for i := 0; i < concW; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []time.Duration
			for time.Now().Before(deadline) {
				start := time.Now()
				_, err := client.RequestSingle[*internal.Response](ctx, c, rpc, nil, &internal.Request{RequestId: rand.NewRequestID()})
				if err != nil {
					errs.Add(1)
					continue
				}
				local = append(local, time.Since(start))
				count.Add(1)
			}
			mu.Lock()
			concLats = append(concLats, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	p50, p95, p99, mx = benchPercentiles(concLats)
	t.Logf("[conc] w=%d d=%v n=%d qps=%.0f err=%d p50=%-10v p95=%-10v p99=%-10v max=%v",
		concW, concDur, count.Load(), float64(count.Load())/concDur.Seconds(), errs.Load(), p50, p95, p99, mx)
}

func benchPercentiles(ds []time.Duration) (p50, p95, p99, max time.Duration) {
	if len(ds) == 0 {
		return
	}
	s := append([]time.Duration(nil), ds...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	pick := func(q float64) time.Duration { return s[int(float64(len(s)-1)*q)] }
	return pick(0.50), pick(0.95), pick(0.99), s[len(s)-1]
}
