# Messaging

Create custom protobuf RPCs built on pub/sub. Supports redis and nats.

## RPCServer

```go
type RPCServer interface {
    // register a rpc handler function
    RegisterHandler(rpc string, handler HandlerFunc) error
    
    // publish updates to a channel (similar to grpc stream)
    Publish(ctx context.Context, channel string, message proto.Message) error
    
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
    SendMultiRequest(ctx context.Context, rpc string, request proto.Message) (<-chan proto.Message, error)
    
    // subscribe to a channel (all subscribed clients will receive every message)
    Subscribe(ctx context.Context, channel string) (Subscription, error)
    
    // subscribe to a channel queue (each message is only received by a single client)
    SubscribeQueue(ctx context.Context, channel string) (Subscription, error)
    
    // close all subscriptions and stop
    Close()
}

type Subscription interface {
    Channel() <-chan proto.Message
    Close() error
}
```

## Simple example

Proto:
```protobuf
message AddRequest {
  int32 number = 1;
}

message AddResult {
  int32 value = 1;
}
```

Server:
```go
type Service struct {
    counter atomic.Int32
}

func (s *Service) AddValue(ctx context.Context, req proto.Message) (proto.Message, error) {
    addRequest := req.(*AddRequest)
    value := counter.Add(addRequest.Number)
    return &AddResult{Value: value}, nil
}

func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcServer := psrpc.NewRPCServer(psrpc.NewMessageBus(rc))
    
    service := &Service{}
    _ = rpcServer.RegisterHandler("AddValue", service.AddValue)
}
```

Client:
```go
func main() {
    rc := redis.NewUniversalClient(&redis.UniversalOptions{Addrs: []string{"localhost:6379"}})
    rpcClient := psrpc.NewRPCClient(psrpc.NewMessageBus(rc))
    res, err := rpcClient.SendSingleRequest(context.Background(), "AddValue", &AddRequest{Number: 3})
    if err != nil {
        return	
    }
    addResult := res.(*AddResult)
    fmt.Println(addResult.Value)
}
```
