# Messaging

Create custom protobuf RPCs built on pub/sub. Supports redis and nats.

## RPCServer

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

## RPCClient

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

For example, the following would return an affinity based on cpu load:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcServer := psrpc.NewRPCServer(psrpc.NewMessageBus(rc))
    _ = rpcServer.RegisterHandler("RunBatchJob", runBatchJob, psrpc.WithAffinityFunc(getAffinity))
    ...
}

func getAffinity(req proto.Message) float32 {
    return stats.GetIdleCPU()
}

func runBatchJob(ctx context.Context, req proto.Message) (proto.Message, error) {
    request := req.(*proto.BatchJobRequest)
    ...
    return &proto.BatchJobResponse{Status: "success"}, nil
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
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcClient := psrpc.NewRPCClient(psrpc.NewMessageBus(rc))
	
    req := &proto.BatchJobRequest{
        Job: "CalculateDAUs",	
    }
    affinityOpts := psrpc.AffinityOpts{
        MinimumAffinity:      0.5,
        AffinityTimeout:      time.Second,
        ShortCircuitTimeout:  time.Millisecond * 250,
    }
    res, err := rpcClient.SendSingleRequest(
        context.Background(), 
        "RunBatchJob", 
        req,
        psrpc.WithAffinityOpts(affinityOpts),
    )
    result := res.(*proto.BatchJobResult)
    fmt.Println(result.Status)
}
```

In the above examples, a server with at least 50% CPU available will be chosen. Once it receives a valid option, it will wait up to 250ms for a better option, and the selection process will take no more than one second.

## Simple Example

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
    rpcServer := psrpc.NewRPCServer(psrpc.NewMessageBus(rc))
    
    service := &Service{}
    _ = rpcServer.RegisterHandler("AddValue", service.AddValue)
    ...
}

type Service struct {
    counter atomic.Int32
}

func (s *Service) AddValue(ctx context.Context, req proto.Message) (proto.Message, error) {
    addRequest := req.(*proto.AddRequest)
    value := counter.Add(addRequest.Increment)
    return &proto.AddResult{Value: value}, nil
}

```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcClient := psrpc.NewRPCClient(psrpc.NewMessageBus(rc))
    res, err := rpcClient.SendSingleRequest(context.Background(), "AddValue", &proto.AddRequest{Increment: 3})
    if err != nil {
        return	
    }
    addResult := res.(*proto.AddResult)
    fmt.Println(addResult.Value)
}
```
