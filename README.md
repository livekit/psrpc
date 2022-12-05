# PubSub-RPC

Create custom protobuf-based golang RPCs built on pub/sub.

Supports:
* Redis or Nats as a backend (or easily [implement your own](#Using-your-own-message-bus))
* Custom server selection for RPC handling based on user-defined [affinity](#Affinity)
* Single RPCs (SendSingleRequest) - one request is handled by one server, used for normal RPCs
* Multi RPCs (SendMultiRequest) - one request is handled by every server, used for distributed updates or result aggregation
* Single RPC streams (JoinStream) - a server can send updates which should only be processed by a single client
* Multi RPC streams (JoinStreamQueue) - a server can send updates which should be processed by every client

## Interfaces

### RPCServer

```go
type RPCServer interface {
    // register a rpc handler function
    RegisterHandler(rpc string, handler HandlerFunc) error
    
    // publish updates to a streaming rpc
    PublishToStream(ctx context.Context, rpc string, message proto.Message) error

    // stop listening for requests for a rpc
    DeregisterHandler(rpc string) error

    // close all subscriptions and stop
    Close()
}

type HandlerFunc func(ctx context.Context, request proto.Message) (proto.Message, error)
```

### RPCClient

```go
type RPCClient interface {
    // send a request to a single server, and receive one response
    SendSingleRequest(ctx context.Context, rpc string, request proto.Message) (proto.Message, error)
    
    // send a request to all servers, and receive one response per server
    SendMultiRequest(ctx context.Context, rpc string, request proto.Message) (<-chan *Response, error)
    
    // subscribe to a streaming rpc (all subscribed clients will receive every message)
    JoinStream(ctx context.Context, rpc string) (Subscription, error)
    
    // join a queue for a streaming rpc (each message is only received by a single client)
    JoinStreamQueue(ctx context.Context, rpc string) (Subscription, error)
    
    // close all subscriptions and stop
    Close()
}

type Response struct {
    Result proto.Message
    Err    error
}

type Subscription interface {
    Channel() <-chan proto.Message
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
    rpcServer.RegisterHandler("MyRPC", rpcHandler, psrpc.WithAffinityFunc(getAffinity))
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
    res, err := rpcClient.SendSingleRequest(
        context.Background(), 
        "MyRPC", 
        req,
        psrpc.WithAffinityOpts(affinityOpts),
    )
```

For a full example, see the [Advanced example](#Advanced) below.

## Examples

### SingleRequest

Proto:
```protobuf
package proto;

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
    rpcServer := psrpc.NewRPCServer("CountingService", psrpc.NewRedisMessageBus(rc))
    
    svc := &CountingService{}
    rpcServer.RegisterHandler("AddValue", svc.AddValue)
    ...
}

type CountingService struct {
    counter atomic.Int32
}

func (s *CountingService) AddValue(ctx context.Context, req proto.Message) (proto.Message, error) {
    addRequest := req.(*proto.AddRequest)
    value := counter.Add(addRequest.Increment)
    return &proto.AddResult{Value: value}, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcClient := psrpc.NewRPCClient("CountingService", psrpc.NewRedisMessageBus(rc))
    res, err := rpcClient.SendSingleRequest(context.Background(), "AddValue", &proto.AddRequest{Increment: 3})
    if err != nil {
        return	
    }
    addResult := res.(*proto.AddResult)
    fmt.Println(addResult.Value)
}
```

### MultiRequest

Proto:
```protobuf
package proto;

message GetValueRequest {}

message ValueResult {
  int32 value = 1;
}
```

Server:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcServer := psrpc.NewRPCServer("CountingService", psrpc.NewRedisMessageBus(rc))
    
    svc := &Service{}
    rpcServer.RegisterHandler("GetAllValues", svc.GetValue)
    ...
}

type Service struct {
    counter atomic.Int32
}

func (s *Service) GetValue(ctx context.Context, req proto.Message) (proto.Message, error) {
    addRequest := req.(*proto.GetValueRequest)
    return &proto.ValueResult{Value: s.counter.Load()}, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcClient := psrpc.NewRPCClient("CountingService", psrpc.NewRedisMessageBus(rc))
    sub, err := rpcClient.SendSingleRequest(context.Background(), "GetAllValues", &proto.GetValueRequest{})
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
            total += response.Result.(*ValueResult).Value	
        }
    }
    fmt.Println("total:", total)
}
```

### Advanced

Proto:
```protobuf
package proto;

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
    rpcServer := psrpc.NewRPCServer("ProcessingService", psrpc.NewRedisMessageBus(rc))
    defer rpcServer.Close()
	
    rpcServer.RegisterHandler("CalculateUserStats", svc.CalculateUserStats, psrpc.WithAffinityFunc(getAffinity))
    ...
}

// The CalculateUserStats rpc requires a lot of CPU - use idle CPUs for the affinity metric
func getAffinity(req proto.Message) float32 {
    return stats.GetIdleCPU()
}

type ProcessingService struct {}

func (s *ProcessingService) CalculateUserStats(ctx context.Context, req proto.Message) (proto.Message, error) {
    request := req.(*proto.CalculateUserStatsRequest)
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
    rpcClient := psrpc.NewRPCClient("ProcessingService", psrpc.NewRedisMessageBus(rc))
    
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
	
    res, err := rpcClient.SendSingleRequest(
        context.Background(),
        "CalculateUserStats",
        req,
        psrpc.WithAffinityOpts(affinityOpts),
        psrpc.WithTimeout(time.Second * 30), // this query takes a while for larger ranges, use a longer timeout
    )
    result := res.(*proto.CalculateUserStatsResponse)
    fmt.Printf("%+v", result)
}
```

## Using your own message bus

To use this RPC system with a message bus other than Redis or Nats, simply implement the MessageBus interface, and plug it into your RPCClient and RPCServer:

```go
type MessageBus interface {
    Publish(ctx context.Context, channel string, msg proto.Message) error
    Subscribe(ctx context.Context, channel string) (Subscription, error)
    SubscribeQueue(ctx context.Context, channel string) (Subscription, error)
}
```

See bus_nats.go and bus_redis.go for example implementations.
