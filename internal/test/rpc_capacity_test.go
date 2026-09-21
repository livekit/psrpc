package test

import (
	"context"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus/bustest"
	"github.com/livekit/psrpc/pkg/client"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/rand"
	"github.com/livekit/psrpc/pkg/server"
)

// TestRPCCapacity is the capacity-grade benchmark: a concurrency sweep with
// repeated rounds per level, reporting QPS mean ± stddev (across rounds) and
// percentiles pooled over all rounds. Gated by BENCH_CAPACITY=1.
//
//	BENCH_WORKERS  comma-separated worker levels (default "1,8,32,128")
//	BENCH_ROUNDS   rounds per level (default 4)
//	BENCH_SECS     round duration (default 8s)
func TestRPCCapacity(t *testing.T) {
	if os.Getenv("BENCH_CAPACITY") == "" {
		t.Skip("set BENCH_CAPACITY=1 to run the capacity benchmark")
	}
	bustest.TestAll(t, benchCapacity)
}

// TestRPCSoak is the stability benchmark: sustained load bucketed per minute
// to expose throughput drift, latency growth or error accumulation over time.
// Gated by BENCH_SOAK=1.
//
//	BENCH_SOAK_WORKERS  concurrent workers (default 16)
//	BENCH_SOAK_SECS     duration (default 10m)
func TestRPCSoak(t *testing.T) {
	if os.Getenv("BENCH_SOAK") == "" {
		t.Skip("set BENCH_SOAK=1 to run the soak benchmark")
	}
	bustest.TestAll(t, benchSoak)
}

// benchPair wires an echo RPC server and an RPC client over two separate bus
// instances, mirroring two processes talking to the same broker.
func benchPair(t *testing.T, busFunc bustest.Connect) *client.RPCClient {
	serviceName := "bench_" + rand.NewString()
	rpc := "echo"

	srv := server.NewRPCServer(&info.ServiceDefinition{Name: serviceName, ID: rand.NewString()}, busFunc(t))
	t.Cleanup(func() { srv.Close(true) })
	c, err := client.NewRPCClient(&info.ServiceDefinition{Name: serviceName, ID: rand.NewString()}, busFunc(t))
	require.NoError(t, err)

	srv.RegisterMethod(rpc, false, false, true, false)
	c.RegisterMethod(rpc, false, false, true, false)
	err = server.RegisterHandler[*internal.Request, *internal.Response](srv, rpc, nil,
		func(ctx context.Context, req *internal.Request) (*internal.Response, error) {
			return &internal.Response{RequestId: req.RequestId}, nil
		}, nil)
	require.NoError(t, err)
	time.Sleep(time.Second) // let subscriptions propagate
	return c
}

func warmup(ctx context.Context, t *testing.T, c *client.RPCClient, n int) {
	for i := 0; i < n; i++ {
		_, err := client.RequestSingle[*internal.Response](ctx, c, "echo", nil, &internal.Request{RequestId: rand.NewRequestID()})
		require.NoError(t, err)
	}
}

// runLoad drives `workers` request loops until the deadline and returns the
// success count, per-request latencies and error count.
func runLoad(ctx context.Context, c *client.RPCClient, workers int, d time.Duration) (int64, []time.Duration, int64) {
	var count, errs atomic.Int64
	var mu sync.Mutex
	var lats []time.Duration
	deadline := time.Now().Add(d)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var local []time.Duration
			for time.Now().Before(deadline) {
				start := time.Now()
				_, err := client.RequestSingle[*internal.Response](ctx, c, "echo", nil, &internal.Request{RequestId: rand.NewRequestID()})
				if err != nil {
					errs.Add(1)
					continue
				}
				local = append(local, time.Since(start))
				count.Add(1)
			}
			mu.Lock()
			lats = append(lats, local...)
			mu.Unlock()
		}()
	}
	wg.Wait()
	return count.Load(), lats, errs.Load()
}

func benchCapacity(t *testing.T, busFunc bustest.Connect) {
	workers := envInts("BENCH_WORKERS", []int{1, 8, 32, 128})
	rounds := envInt("BENCH_ROUNDS", 4)
	secs := envDuration("BENCH_SECS", 8*time.Second)

	c := benchPair(t, busFunc)
	ctx := context.Background()
	warmup(ctx, t, c, 20)

	for _, w := range workers {
		var qpsList []float64
		var pooled []time.Duration
		var totalErrs int64
		for r := 0; r < rounds; r++ {
			n, lats, errs := runLoad(ctx, c, w, secs)
			qpsList = append(qpsList, float64(n)/secs.Seconds())
			pooled = append(pooled, lats...)
			totalErrs += errs
		}
		mean, std := meanStd(qpsList)
		p50, p95, p99, mx := benchPercentiles(pooled)
		t.Logf("[cap ] w=%-4d rounds=%d×%v qps=%6.0f±%-5.0f p50=%-10v p95=%-10v p99=%-10v max=%-10v errs=%d",
			w, rounds, secs, mean, std, p50, p95, p99, mx, totalErrs)
	}
}

func benchSoak(t *testing.T, busFunc bustest.Connect) {
	workers := envInt("BENCH_SOAK_WORKERS", 16)
	dur := envDuration("BENCH_SOAK_SECS", 10*time.Minute)

	c := benchPair(t, busFunc)
	ctx := context.Background()
	warmup(ctx, t, c, 20)

	type bucket struct {
		count int64
		errs  int64
		lats  []time.Duration
	}
	nBuckets := int(dur/time.Minute) + 1
	buckets := make([]*bucket, nBuckets)
	var bmu sync.Mutex

	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Since(start) < dur {
				t0 := time.Now()
				_, err := client.RequestSingle[*internal.Response](ctx, c, "echo", nil, &internal.Request{RequestId: rand.NewRequestID()})
				dt := time.Since(t0)

				bi := int(time.Since(start) / time.Minute)
				bmu.Lock()
				bk := buckets[bi]
				if bk == nil {
					bk = &bucket{}
					buckets[bi] = bk
				}
				if err != nil {
					bk.errs++
				} else {
					bk.count++
					bk.lats = append(bk.lats, dt)
				}
				bmu.Unlock()
			}
		}()
	}
	wg.Wait()

	for i, bk := range buckets {
		if bk == nil || bk.count == 0 {
			continue
		}
		p50, p95, p99, mx := benchPercentiles(bk.lats)
		t.Logf("[soak] min=%02d qps=%-6.0f p50=%-10v p95=%-10v p99=%-10v max=%-10v errs=%d",
			i, float64(bk.count)/60, p50, p95, p99, mx, bk.errs)
	}
}

func meanStd(xs []float64) (mean, std float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	for _, x := range xs {
		std += (x - mean) * (x - mean)
	}
	std = math.Sqrt(std / float64(len(xs)))
	return mean, std
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envInts(key string, def []int) []int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	var out []int
	for _, p := range strings.Split(v, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(p)); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return def
	}
	return out
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
