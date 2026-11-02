# psrpc threat model

`github.com/livekit/psrpc` is a Go RPC framework that runs over a pub/sub
message bus (redis, NATS, or an in-process local bus). Services define RPCs in
`.proto` files; `protoc-gen-psrpc` generates typed clients and servers. It
supports single-response RPCs, multi-response RPCs, queue RPCs, server
affinity and claims, topics, and bidirectional streams. LiveKit server
(`livekit/livekit`) and other LiveKit services use it to talk to each other.

## Scope and trust boundaries

The bus is trusted infrastructure. psrpc does not authenticate messages:
`ClientId`, `ServerId` and `RequestId` are self-asserted, and anyone who can
publish on the bus can impersonate any client or server. Authenticating peers
and restricting bus access is the deployment's job. A finding that needs only
"publish a forged message as another peer" is not a vulnerability here.

The attacker model that remains:

- **Malformed or hostile messages from a peer.** A peer may run a different
  psrpc version, have a bug, or be partly compromised. A single message must
  not crash, deadlock, or leak resources in the process that receives it.
  Messages may arrive duplicated, reordered, late, after expiry, or after the
  receiver has closed.
- **Untrusted strings in topics.** Embedding applications build topics from
  values that end users influence (for example room names or participant
  identities). Channel-name construction must keep such a value inside its
  own channel.
- **Request payloads** that the embedding application received from end users
  and passes through psrpc unchanged.

Audit the hand-written Go on `main`. Prioritise:

- `pkg/bus/`: `Deserialize`, `DecodeLocalChannel`, `gunzip` and the
  decompression cap, subscription forwarding, and the `Transport` contract.
- `pkg/bus/natsbus/`, `pkg/bus/redisbus/`, `pkg/bus/localbus/`: routing on the
  `Local` channel, queue dispatch, inbox and no-responder handling,
  subscribe/unsubscribe races.
- `pkg/server/`, `pkg/client/`, `internal/stream/`: request expiry, claim
  negotiation, the claim and stream maps, per-request goroutines, stream
  open/ack/close, and graceful versus forced close.
- `pkg/info/channels.go`: channel naming and `appendSanitizedChannelPart`.
- `errors.go`, `pkg/metadata/`, `pkg/middleware/`: error and detail
  round-tripping, metadata propagation, and the retry middleware.
- `protoc-gen-psrpc/`: generator bugs that make generated code unsafe. Its
  input (`.proto` files) is trusted developer input.

Out of scope: generated code (`*.pb.go`, generated `*.psrpc.go`), test
helpers under `testutils/` and `pkg/bus/bustest/`, `magefile.go`, and bugs in
redis, NATS, or their Go clients.

Trusted: the bus and its credentials, the caller's options and configuration,
service and method names (they come from `.proto` files), and handlers
registered by the embedding application.

LiveKit server (`livekit/livekit`) is enrolled separately, and
`livekit/protocol` has its own threat model. Report bugs in how they use psrpc there. Report bugs in psrpc
itself here.

## Security properties

- **Robust decoding:** a malformed, truncated, wrong-type, or unknown-type
  message must be dropped or answered with an error. It must not panic, block
  the subscription loop, or stop the receiver from handling later messages.
- **Bounded decompression:** when `MaxDecompressedSize` is set, no compressed
  message may expand beyond it. The default is zero, which disables the cap.
- **Lifecycle:** every request, claim and stream must end in a response, an
  error, or a timeout no later than its expiry. Closing a client, server or
  stream must not deadlock, panic on a closed channel, or leave goroutines,
  timers or map entries behind. Duplicated or late messages must not block a
  handler's run loop.
- **Delivery semantics:** a queue RPC must run on at most one server. A
  response must reach only the request that it answers. A claim must be granted
  to at most one server.
- **Channel isolation:** a topic must not inject channel separators or
  broker wildcards (`.`, `|`, `*`, `>`), or otherwise collide with another
  topic's channel.

## Severity guidance

- **High:** a topic value that routes requests or responses to another
  topic's channel; a single message from a peer that crashes the receiving
  process or permanently deadlocks a server or client.
- **Medium:** a single peer message that leaks goroutines, memory or map
  entries without bound; a decompression cap that can be exceeded; a queue RPC
  handled twice, or a response delivered to the wrong request, under ordinary
  timing.
- **Low:** races that need unusual timing and only fail one request; leaks
  that need many malformed messages.
- **Not a finding:** forging messages as another peer; anything that needs
  control of the bus or its credentials; memory use from compressed messages
  when `MaxDecompressedSize` is zero; broker outages and reconnection
  behaviour without a correctness impact; issues only in generated or test
  code.

## Testing

The checkout is at `/src`. Go dependencies are cached. `GOPROXY=off` and
`GOSUMDB=off` prevent downloads. The `internal/test` `*.psrpc.go` fixtures were
generated during the build. After changing `protoc-gen-psrpc`, regenerate
them with `go tool mage generate && git checkout -- internal/test`. `protoc`
is installed. The checkout restores the committed `*.pb.go` files, which the
generator rewrites.

The bundled redis and NATS tests (`TestRedisMessageBus`, `TestInbox`,
`TestNoSubscriberFailsFast`, `TestServerAnswersPerRequest`, and the `NATS` and
`Redis` subtests of the `bustest` conformance suite) start brokers through
Docker. No Docker daemon is available, so skip them. The local-bus subtests of
the same suite still run:

```sh
cd /src
go test -count=1 -skip '^(TestInbox|TestRedisMessageBus)$' \
  $(go list ./... | grep -v -e '/internal/test$' -e '/internal/test/my_service$' -e '/pkg/bus$')
go test -count=1 -skip '/^(NATS|Redis)$' ./pkg/bus ./internal/test/my_service
go test -count=1 -run '/^Local$' \
  -skip '^(TestNoSubscriberFailsFast|TestServerAnswersPerRequest)$' ./internal/test
```

Add `-race` to any of these for lifecycle and concurrency bugs.

`redis-server` and `nats-server` are installed for hand-written transport
reproducers. The bundled tests do not use them. Start them and connect with
`redisbus.New` or `natsbus.New`:

```sh
redis-server --daemonize yes          # localhost:6379
nats-server -a 127.0.0.1 -p 4222 &    # nats://127.0.0.1:4222
```

There are no fuzz tests. `bus.Deserialize`, `bus.DecodeLocalChannel` and
`internal/stream` `HandleStream` are natural fuzz targets.

## Reports

State what the attacker controls (a single peer message, a topic string, or a
request payload), the entry point, and the demonstrated impact. Provide a
minimal reproducer, preferably a Go test against `localbus`. Use redis or NATS
only when the bug is in that transport.
