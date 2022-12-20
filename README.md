# PubSub-RPC

Create custom protobuf-based golang RPCs built on pub/sub.

Supports:
* Redis or Nats as a backend
* Custom server selection for RPC handling based on user-defined [affinity](#Affinity)
* Single RPCs (RequestSingle) - one request is handled by one server, used for normal RPCs
* Multi RPCs (RequestAll) - one request is handled by every server, used for distributed updates or result aggregation
* Single RPC streams (JoinStreamQueue) - updates sent from the server will only be processed by a single client
* Multi RPC streams (JoinStream) - updates sent be the server will be processed by every client

## Interfaces

### RPCServer

```go
type RPCServer interface {
    // register a handler
    RegisterHandler(h Handler) error
    // publish updates to a streaming rpc
    PublishToStream(ctx context.Context, rpc string, message proto.Message) error
    // stop listening for requests for a rpc
    DeregisterHandler(rpc string) error
    // close all subscriptions and stop
    Close()
}
```

### RPCClient

```go
type RPC[RequestType proto.Message, ResponseType proto.Message] interface {
    // send a request to a single server, and receive one response
    RequestSingle(ctx context.Context, request RequestType, opts ...RequestOption) (ResponseType, error)
    // send a request to all servers, and receive one response per server
    RequestAll(ctx context.Context, request RequestType, opts ...RequestOption) (<-chan *Response[ResponseType], error)
    // subscribe to a streaming rpc (all subscribed clients will receive every message)
    JoinStream(ctx context.Context, rpc string) (Subscription[ResponseType], error)
    // join a queue for a streaming rpc (each message is only received by a single client)
    JoinStreamQueue(ctx context.Context, rpc string) (Subscription[ResponseType], error)
}

type RPCClient interface {
    // close all subscriptions and stop
    Close()
}

type Response[ResponseType proto.Message] struct {
    Result ResponseType
    Err    error
}

type Subscription[MessageType proto.Message] interface {
    Channel() <-chan MessageType
    Close() error
}
```

## Affinity

### AffinityFunc

The server can register an AffinityFunc for the client to decide which instance should take a SingleRequest.
A higher affinity score is better, and a score of 0 means the server is not available.

For example, the following could be used to return an affinity based on cpu load:
```go
func main() {
    ...
    rpcServer.RegisterHandler(psrpc.NewHandler("MyRPC", handlerFunc).WithAffinityFunc(getAffinity))
    ...
}

func getAffinity(req proto.Message) float32 {
    return stats.GetIdleCPU()
}
```

### AffinityOptions

On the client side, you can also set affinity options for server selection with SendSingleRequest.

Affinity options:
* AcceptFirstAvailable: if true, the first server to respond with an affinity greater than 0 and MinimumAffinity will be selected.
* MinimumAffinity: Minimum affinity for a server to be considered a valid handler. 
* AffinityTimeout: if AcceptFirstAvailable is false, the client will wait this amount of time, then select the server with the highest affinity score.
* ShortCircuitTimeout: if AcceptFirstAvailable is false, the client will wait this amount of time after receiving its first valid response, then select the server with the highest affinity score.

If no options are supplied, the client will choose the first server to respond with an affinity score > 0.

```go
affinityOpts := psrpc.AffinityOpts{
    MinimumAffinity:      0.5,
    AffinityTimeout:      time.Second,
    ShortCircuitTimeout:  time.Millisecond * 250,
}
res, err := myRPC.RequestSingle(
    context.Background(), 
    req,
    psrpc.WithAffinityOpts(affinityOpts),
)
```

For a full example, see the [Advanced example](#Advanced) below.

## Examples

### SingleRequest

Proto:
```protobuf
package api;

message AddRequest {
  int32 increment = 1;
}

message AddResult {
  int32 value = 1;
}
```

Server:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    serverID := "test_server"
    rpcServer := psrpc.NewRPCServer("CountingService", serverID, psrpc.NewRedisMessageBus(rc))
    
    svc := &CountingService{}
    rpcServer.RegisterHandler(psrpc.NewHandler("AddValue", svc.AddValue))
    ...
}

type CountingService struct {
    counter atomic.Int32
}

func (s *CountingService) AddValue(ctx context.Context, req api.AddReqest) (api.AddResult, error) {
    value := counter.Add(req.Increment)
    return &proto.AddResult{Value: value}, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    clientID := "test_client"
    rpcClient := psrpc.NewRPCClient("CountingService", clientID, psrpc.NewRedisMessageBus(rc))
	
    addValue := psrpc.NewRPC[*api.AddRequest, *api.AddResult](rpcClient, "AddValue") 
    res, err := addValue.RequestSingle(context.Background(), &proto.AddRequest{Increment: 3})
    if err != nil {
        return	
    }
    fmt.Println(res.Value)
}
```

### MultiRequest

Proto:
```protobuf
package api;

message GetValueRequest {}

message ValueResult {
  int32 value = 1;
}
```

Server:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    serverID := "test_server"
    rpcServer := psrpc.NewRPCServer("CountingService", serverID, psrpc.NewRedisMessageBus(rc))
    
    svc := &Service{}
    rpcServer.RegisterHandler(psrpc.NewHandler("GetValue", svc.GetValue))
    ...
}

type Service struct {
    counter atomic.Int32
}

func (s *Service) GetValue(ctx context.Context, req *api.GetValueRequest) (*api.ValueResult, error) {
    return &proto.ValueResult{Value: s.counter.Load()}, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    clientID := "test_client"
    rpcClient := psrpc.NewRPCClient("CountingService", clientID, psrpc.NewRedisMessageBus(rc))
	
    getValues := psrpc.NewRPC[*api.GetValueRequest, *api.ValueResult](rpcClient, "GetValue")
    sub, err := getValues.RequestAll(context.Background(), &proto.GetValueRequest{})
    if err != nil {
        return	
    }
    defer sub.Close()
    
    total := 0
    for {
        response, ok := <-sub.Channel()
        if !ok {
            // channel has been closed
            break
        }
		
        if response.Err != nil {
            fmt.Println("error:", err)	
        } else {
            total += response.Result.Value	
        }
    }
    fmt.Println("total:", total)
}
```

### Advanced

Proto:
```protobuf
package api;

message CalculateUserStatsRequest {
  int64 from_unix_time = 1;
  int64 to_unix_time = 2;
}

message CalculateUserStatsResponse {
  int32 unique_users = 1;
  int32 new_users = 2;
  int32 inactive_users = 3;
  int32 daily_active_users = 4;
}
```

Server:
```go
func main() {
    svc := NewProcessingService()
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    serverID := "test_server"
    rpcServer := psrpc.NewRPCServer("ProcessingService", serverID, psrpc.NewRedisMessageBus(rc))
    defer rpcServer.Close()
	
    rpcServer.RegisterHandler(psrpc.NewHandler("CalculateUserStats", svc.CalculateUserStats).WithAffinityFunc(getAffinity))
    ...
}

// The CalculateUserStats rpc requires a lot of CPU - use idle CPUs for the affinity metric
func getAffinity(req proto.Message) float32 {
    return stats.GetIdleCPU()
}

type ProcessingService struct {}

func (s *ProcessingService) CalculateUserStats(ctx context.Context, req *api.CalculateUserStatsRequest) (*api.CalculateUserStatsResponse, error) {
    userStats, err := getUserStats(req.FromUnixTime, req.ToUnixTime)
    if err != nil {
        return nil, err
    }
	
    return &proto.CalculateUserStatsResponse{
        UniqueUsers: userStats.uniqueUsers,
        NewUsers: userStats.newUsers,
        InactiveUsers: userStats.inactiveUsers,
        DailyActiveUsers: userStats.dailyActiveUsers,
    }, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    clientID := "test_client"
    rpcClient := psrpc.NewRPCClient("ProcessingService", clientID, psrpc.NewRedisMessageBus(rc))
	
    calculateStats := psrpc.NewRPC[*api.CalculateUserStatsRequest, *api.CalculateUserStatsResponse](rpcClient, "CalculateUserStats")
    
    req := &proto.CalculateUserStatsRequest{
        FromUnixTime: time.Now().Add(-time.Day * 30).UnixNano(),
        ToUnixTime: time.Now().UnixNano()
    }
	
    // allow one second for server selection, with at least 0.5 cpu available
    // once the first valid server is found, selection will occur 250ms later 
    affinityOpts := psrpc.AffinityOpts{
        MinimumAffinity:      0.5,
        AffinityTimeout:      time.Second,
        ShortCircuitTimeout:  time.Millisecond * 250,
    }
	
    res, err := calculateStats.RequestSingle(
        context.Background(),
        req,
        psrpc.WithAffinityOpts(affinityOpts),
        psrpc.WithTimeout(time.Second * 30), // this query takes a while for larger ranges, use a longer timeout
    )
    fmt.Printf("%+v", res)
}
```
