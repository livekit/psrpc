#!/usr/bin/env bash
set -euo pipefail

protoc --go_out=. --psrpc_out=. \
    --go_opt=paths=source_relative \
    --go_opt=My/y.proto=github.com/livekit/psrpc/internal/test/importmapping/y \
    --psrpc_opt=paths=source_relative \
    --psrpc_opt=My/y.proto=github.com/livekit/psrpc/internal/test/importmapping/y \
    y/y.proto

protoc --go_out=. --psrpc_out=. \
    --go_opt=paths=source_relative \
    --go_opt=My/y.proto=github.com/livekit/psrpc/internal/test/importmapping/y \
    --go_opt=Mx/x.proto=github.com/livekit/psrpc/internal/test/importmapping/x \
    --psrpc_opt=paths=source_relative \
    --psrpc_opt=My/y.proto=github.com/livekit/psrpc/internal/test/importmapping/y \
    --psrpc_opt=Mx/x.proto=github.com/livekit/psrpc/internal/test/importmapping/x \
    x/x.proto
