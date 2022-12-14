# PubSub-RPC

Create custom protobuf-based golang RPCs built on pub/sub.

Supports:
* Protobuf service definitions
* Use Redis, Nats, or a local communication layer
* Custom server selection for RPC handling based on user-defined [affinity](#Affinity)
* RPC topics - any RPC can be divided into topics, (e.g. by region)
* Single RPCs - one request is handled by one server, used for normal RPCs
* Multi RPCs - one request is handled by every server, used for distributed updates or result aggregation
* Queue Subscriptions - updates sent from the server will only be processed by a single client
* Subscriptions - updates sent be the server will be processed by every client

## Usage

### Protobuf

PSRPC is generated from proto files, and we've added a few custom method options:
```protobuf
message Options {
  // For RPCs, each client request will receive a response from every server.
  // For subscriptions, every client will receive every update.
  bool multi = 1;

  // This method is a pub/sub.
  bool subscription = 2;

  // This method uses topics.
  bool topics = 3;

  // Your service will supply an affinity function for handler selection.
  bool affinity_func = 4;
}

```

Start with your service definition. Here's an example using different method options: 

```protobuf
syntax = "proto3";

import "options.proto";

option go_package = "/api";

service MyService {
  // A normal RPC - one request, one response. The request will be handled by the first available server
  rpc NormalRPC(MyRequest) returns (MyResponse);
  
  // An RPC with a server affinity function for handler selection.
  rpc IntensiveRPC(MyRequest) returns (MyResponse) {
    option (psrpc.options).affinity_func = true;
  };
  
  // A multi-rpc - a client will send one request, and receive one response each from every server
  rpc GetStats(MyRequest) returns (MyResponse) {
    option (psrpc.options).multi = true;
  };
  
  // An RPC with topics - a client can send one request, and receive one response from each server in one region
  rpc GetRegionStats(MyRequest) returns (MyResponse) {
    option (psrpc.options).topics = true;
    option (psrpc.options).multi = true;
  }
  
  // A queue subscription - even if multiple clients are subscribed, only one will receive this update.
  // The request parameter (in this case, Ignored) will always be ignored when generating go files.
  rpc ProcessUpdate(Ignored) returns (MyUpdate) {
    option (psrpc.options).subscription = true;
  };
  
  // A normal subscription - every client will receive every update.
  // The request parameter (in this case, Ignored) will always be ignored when generating go files.
  rpc UpdateState(Ignored) returns (MyUpdate) {
    option (psrpc.options).subscription = true;
    option (psrpc.options).multi = true;
  };

  // A subscription with topics - every client subscribed to the topic will receive every update.
  // The request parameter (in this case, Ignored) will always be ignored when generating go files.
  rpc UpdateRegionState(Ignored) returns (MyUpdate) {
    option (psrpc.options).subscription = true;
    option (psrpc.options).topics = true;
    option (psrpc.options).multi = true;
  }
}

message Ignored {}
message MyRequest {}
message MyResponse {}
message MyUpdate {}
```

### Generation

Install `protoc-gen-psrpc` by running `go install github.com/livekit/psrpc/protoc-gen-psrpc`.

If using the custom options above, you'll also need to include [options.proto](protoc-gen-psrpc/options/options.proto).
The simplest way to do this is to include psrpc in your project, then run
```shell
go list -json -m github.com/livekit/psrpc

{
	"Path": "github.com/livekit/psrpc",
	"Version": "v0.2.2",
	"Time": "2022-12-27T21:40:05Z",
	"Dir": "/Users/dc/go/pkg/mod/github.com/livekit/psrpc@v0.2.2",
	"GoMod": "/Users/dc/go/pkg/mod/cache/download/github.com/livekit/psrpc/@v/v0.2.2.mod",
	"GoVersion": "1.18"
}
```

Use the `--psrpc_out` with `protoc` and include the options directory.

```shell
protoc \ 
  --go_out=paths=source_relative:. \
  --psrpc_out=paths=source_relative:. \
  -I /Users/dc/go/pkg/mod/github.com/livekit/psrpc@v0.2.2/protoc-gen-psrpc/options \
  -I=. my_service.proto
```

This will create a `my_service.psrpc.go` file.

### Client

A `MyServiceClient` will be generated based on your rpc definitions:

```go
type MyServiceClient interface {
    // A normal RPC - one request, one response. The request will be handled by the first available server
    NormalRPC(ctx context.Context, req *MyRequest, opts ...psrpc.RequestOpt) (*MyResponse, error)
    
    // An RPC with a server affinity function for handler selection.
    IntensiveRPC(ctx context.Context, req *MyRequest, opts ...psrpc.RequestOpt) (*MyResponse, error)
    
    // A multi-rpc - a client will send one request, and receive one response each from every server
    GetStats(ctx context.Context, req *MyRequest, opts ...psrpc.RequestOpt) (<-chan *psrpc.Response[*MyResponse], error)
    
    // An RPC with topics - a client can send one request, and receive one response from each server in one region
    GetRegionStats(ctx context.Context, topic string, req *Request, opts ...psrpc.RequestOpt) (<-chan *psrpc.Response[*MyResponse], error)
    
    // A queue subscription - even if multiple clients are subscribed, only one will receive this update.
    SubscribeProcessUpdate(ctx context.Context) (psrpc.Subscription[*MyUpdate], error)
    
    // A subscription with topics - every client subscribed to the topic will receive every update.
    SubscribeUpdateRegionState(ctx context.Context, topic string) (psrpc.Subscription[*MyUpdate], error)
}

// NewMyServiceClient creates a psrpc client that implements the MyServiceClient interface.
func NewMyServiceClient(clientID string, bus psrpc.MessageBus, opts ...psrpc.ClientOpt) (MyServiceClient, error) {
    ...
}
```

Multi-RPCs will return a `chan *psrpc.Response`, where you will receive an individual response or error from each server:
```go
type Response[ResponseType proto.Message] struct {
    Result ResponseType
    Err    error
}
```

Subscription RPCs will return a `psrpc.Subscription`, where you can listen for updates on its channel:

```go
type Subscription[MessageType proto.Message] interface {
    Channel() <-chan MessageType
    Close() error
}
```

### ServerImpl

A `<ServiceName>ServerImpl` interface will be also be generated from your rpcs. Your service will need to fulfill its interface:

```go
type MyServiceServerImpl interface {
    // A normal RPC - one request, one response. The request will be handled by the first available server
    NormalRPC(ctx context.Context, req *MyRequest) (*MyResponse, error)
    
    // An RPC with a server affinity function for handler selection.
    IntensiveRPC(ctx context.Context, req *MyRequest) (*MyResponse, error)
    IntensiveRPCAffinity(req *MyRequest) float32
    
    // A multi-rpc - a client will send one request, and receive one response each from every server
    GetStats(ctx context.Context, req *MyRequest) (*MyResponse, error)
    
    // An RPC with topics - a client can send one request, and receive one response from each server in one region
    GetRegionStats(ctx context.Context, req *MyRequest) (*MyResponse, error)
}
```

### Server

Finally, a `<ServiceName>Server` will be generated. This is used to start your rpc server, as well as register and deregister topics:

```go
type MyServiceServer interface {
    // An RPC with topics - a client can send one request, and receive one response from each server in one region
    RegisterGetRegionStatsTopic(topic string) error
    DeregisterGetRegionStatsTopic(topic string) error
    
    // A queue subscription - even if multiple clients are subscribed, only one will receive this update.
    PublishProcessUpdate(ctx context.Context, msg *MyUpdate) error
    
    // A subscription with topics - every client subscribed to the topic will receive every update.
    PublishUpdateRegionState(ctx context.Context, topic string, msg *MyUpdate) error

    // Close and wait for pending RPCs to complete
    Shutdown()
    
    // Close immediately, without waiting for pending RPCs
    Kill()
}

// NewMyServiceServer builds a RPCServer that can be used to handle
// requests that are routed to the right method in the provided svc implementation.
func NewMyServiceServer(serverID string, svc MyServiceServerImpl, bus psrpc.MessageBus, opts ...psrpc.ServerOpt) (MyServiceServer, error) {
    ...
}
```

## Affinity

### AffinityFunc

The server can implement an affinity function for the client to decide which instance should take a SingleRequest.
A higher affinity score is better, and a score of 0 means the server is not available.

For example, the following could be used to return an affinity based on cpu load:
```protobuf
rpc IntensiveRPC(MyRequest) returns (MyResponse) {
  option (psrpc.options).affinity_func = true;
};
```

```go
func (s *MyService) IntensiveRPC(ctx context.Context, req *api.MyRequest) (*api.MyResponse, error) {
    ... // do something CPU intensive
}

func (s *MyService) IntensiveRPCAffinity(_ *MyRequest) float32 {
    return stats.GetIdleCPU()
}
```

### SelectionOpts

On the client side, you can also set server selection options with single RPCs.

```go
type SelectionOpts struct {
    MinimumAffinity      float32       // (default 0) minimum affinity for a server to be considered a valid handler
    AcceptFirstAvailable bool          // (default true)
    AffinityTimeout      time.Duration // (default 0 (none)) server selection deadline
    ShortCircuitTimeout  time.Duration // (default 0 (none)) deadline imposed after receiving first response
}
```

```go
selectionOpts := psrpc.SelectionOpts{
    MinimumAffinity:      0.5,
    AffinityTimeout:      time.Second,
    ShortCircuitTimeout:  time.Millisecond * 250,
}

res, err := myClient.IntensiveRPC(ctx, req, psrpc.WithSelectionOpts(selectionOpts))
```
