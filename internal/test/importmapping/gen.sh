#!/usr/bin/env bash
# Copyright 2023 LiveKit, Inc.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

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
