package test

import (
	"context"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lithammer/shortuuid/v3"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/test/my_service"
)

func TestGeneratedService(t *testing.T) {
	t.Run("Local", func(t *testing.T) {
		testGeneratedService(t, psrpc.NewLocalMessageBus())
	})

	t.Run("Redis", func(t *testing.T) {
		rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
		testGeneratedService(t, psrpc.NewRedisMessageBus(rc))
	})

	t.Run("Nats", func(t *testing.T) {
		nc, _ := nats.Connect(nats.DefaultURL)
		testGeneratedService(t, psrpc.NewNatsMessageBus(nc))
	})
}

func testGeneratedService(t *testing.T, bus psrpc.MessageBus) {
	ctx := context.Background()
	req := &my_service.MyRequest{}
	update := &my_service.MyUpdate{}
	sA := createServer(t, bus)
	sB := createServer(t, bus)
	cA := createClient(t, bus)
	cB := createClient(t, bus)

	// rpc NormalRPC(MyRequest) returns (MyResponse);
	_, err := cA.NormalRPC(ctx, req)
	require.NoError(t, err)

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["NormalRPC"]+sB.counts["NormalRPC"])
	sA.Unlock()
	sB.Unlock()

	// rpc IntensiveRPC(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).affinity_func = true;
	_, err = cB.IntensiveRPC(ctx, req)
	require.NoError(t, err)

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["IntensiveRPC"]+sB.counts["IntensiveRPC"])
	require.Equal(t, 1, sA.counts["IntensiveRPCAffinity"])
	require.Equal(t, 1, sB.counts["IntensiveRPCAffinity"])
	sA.Unlock()
	sB.Unlock()

	// rpc GetStats(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).multi = true;
	respChan, err := cA.GetStats(ctx, req)
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		select {
		case res := <-respChan:
			require.NoError(t, res.Err)
		case <-time.After(time.Second * 3):
			t.Fatalf("timed out")
		}
	}

	sA.Lock()
	sB.Lock()
	require.Equal(t, 1, sA.counts["GetStats"])
	require.Equal(t, 1, sB.counts["GetStats"])
	sA.Unlock()
	sB.Unlock()

	// rpc GetRegionStats(MyRequest) returns (MyResponse) {
	//   option (psrpc.options).topics = true;
	//   option (psrpc.options).multi = true;
	require.NoError(t, sA.server.RegisterGetRegionStatsTopic("regionA"))
	require.NoError(t, sA.server.RegisterGetRegionStatsTopic("regionB"))
	sA.server.DeregisterGetRegionStatsTopic("regionB")
	require.NoError(t, sB.server.RegisterGetRegionStatsTopic("regionB"))
	time.Sleep(time.Millisecond * 100)

	respChan, err = cB.GetRegionStats(ctx, "regionB", req)
	require.NoError(t, err)
	select {
	case res := <-respChan:
		require.NotNil(t, res)
		require.NoError(t, res.Err)
	case <-time.After(time.Second):
		t.Fatalf("timed out")
	}

	sA.Lock()
	sB.Lock()
	require.Equal(t, 0, sA.counts["GetRegionStats"])
	require.Equal(t, 1, sB.counts["GetRegionStats"])
	sA.Unlock()
	sB.Unlock()

	// rpc ProcessUpdate(Ignored) returns (MyUpdate) {
	//   option (psrpc.options).subscription = true;
	subA, err := cA.SubscribeProcessUpdate(ctx)
	require.NoError(t, err)
	subB, err := cB.SubscribeProcessUpdate(ctx)
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	require.NoError(t, sA.server.PublishProcessUpdate(ctx, update))
	requireOne(t, subA, subB)
	require.NoError(t, subA.Close())
	require.NoError(t, subB.Close())

	// rpc UpdateRegionState(Ignored) returns (MyUpdate) {
	//   option (psrpc.options).subscription = true;
	//   option (psrpc.options).topics = true;
	//   option (psrpc.options).multi = true;
	subA, err = cA.SubscribeUpdateRegionState(ctx, "regionA")
	require.NoError(t, err)
	subB, err = cB.SubscribeUpdateRegionState(ctx, "regionA")
	require.NoError(t, err)
	time.Sleep(time.Millisecond * 100)

	require.NoError(t, sB.server.PublishUpdateRegionState(ctx, "regionA", update))
	requireTwo(t, subA, subB)
	require.NoError(t, subA.Close())
	require.NoError(t, subB.Close())

	shutdown(t, sA)
	shutdown(t, sB)
}

func requireOne(t *testing.T, subA, subB psrpc.Subscription[*my_service.MyUpdate]) {
	for i := 0; i < 2; i++ {
		select {
		case <-subA.Channel():
			if i == 0 {
				continue
			}
		case <-subB.Channel():
			if i == 0 {
				continue
			}
		case <-time.After(time.Second):
			if i == 1 {
				continue
			}
		}
		t.Fatalf("%d responses received", i*2)
	}
}

func requireTwo(t *testing.T, subA, subB psrpc.Subscription[*my_service.MyUpdate]) {
	for i := 0; i < 2; i++ {
		select {
		case <-subA.Channel():
		case <-subB.Channel():
		case <-time.After(time.Second):
			t.Fatalf("timed out")
		}
	}
}

func createServer(t *testing.T, bus psrpc.MessageBus) *MyService {
	svc := &MyService{
		counts: make(map[string]int),
	}
	server, err := my_service.NewMyServiceServer(newID(), svc, bus)
	require.NoError(t, err)
	svc.server = server
	return svc
}

func createClient(t *testing.T, bus psrpc.MessageBus) my_service.MyServiceClient {
	client, err := my_service.NewMyServiceClient(newID(), bus)
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
		t.Fatalf("shutdown not returning")
	}
}

type MyService struct {
	sync.Mutex

	server my_service.MyServiceServer
	counts map[string]int
}

func (s *MyService) NormalRPC(_ context.Context, _ *my_service.MyRequest) (*my_service.MyResponse, error) {
	s.Lock()
	s.counts["NormalRPC"]++
	s.Unlock()
	return &my_service.MyResponse{}, nil
}

func (s *MyService) IntensiveRPC(_ context.Context, _ *my_service.MyRequest) (*my_service.MyResponse, error) {
	s.Lock()
	s.counts["IntensiveRPC"]++
	s.Unlock()
	return &my_service.MyResponse{}, nil
}

func (s *MyService) IntensiveRPCAffinity(_ *my_service.MyRequest) float32 {
	s.Lock()
	s.counts["IntensiveRPCAffinity"]++
	s.Unlock()
	return rand.Float32()
}

func (s *MyService) GetStats(_ context.Context, _ *my_service.MyRequest) (*my_service.MyResponse, error) {
	s.Lock()
	s.counts["GetStats"]++
	s.Unlock()
	return &my_service.MyResponse{}, nil
}

func (s *MyService) GetRegionStats(_ context.Context, _ *my_service.MyRequest) (*my_service.MyResponse, error) {
	s.Lock()
	s.counts["GetRegionStats"]++
	s.Unlock()
	return &my_service.MyResponse{}, nil
}

func newID() string {
	return shortuuid.New()[:12]
}
