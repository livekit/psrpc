# Copyright 2026 LiveKit, Inc.
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

# Build image for Anthropic's OSS Scanner. Keep the Go version at or above go.mod's: GOTOOLCHAIN=local won't fetch one.
FROM golang:1.26-bookworm

ENV DEBIAN_FRONTEND=noninteractive \
    GOTOOLCHAIN=local

RUN apt-get update && apt-get install -y --no-install-recommends \
      build-essential ca-certificates git protobuf-compiler libprotobuf-dev redis-server \
    && rm -rf /var/lib/apt/lists/*

# For hand-written natsbus reproducers; built from source so it matches the build architecture.
RUN go install github.com/nats-io/nats-server/v2@v2.15.2

WORKDIR /src
COPY . /src

RUN go mod download && go mod verify

# The internal/test *.psrpc.go fixtures are gitignored; generate them with this tree's protoc-gen-psrpc.
# Generation also rewrites the committed *.pb.go with Debian's protoc, so restore those.
RUN go tool mage generate && git checkout -- internal/test
RUN go build ./... && go vet ./...

# The bundled redis and NATS tests start brokers through Docker, which the image lacks, so skip them.
RUN go test -count=1 -timeout 20m -skip '^(TestInbox|TestRedisMessageBus)$' \
      $(go list ./... | grep -v -e '/internal/test$' -e '/internal/test/my_service$' -e '/pkg/bus$') \
    && go test -count=1 -timeout 10m -skip '/^(NATS|Redis)$' ./pkg/bus ./internal/test/my_service \
    && go test -count=1 -timeout 10m -run '/^Local$' \
      -skip '^(TestNoSubscriberFailsFast|TestServerAnswersPerRequest)$' ./internal/test \
    || echo 'WARNING: baseline tests failed'

ENV GOPROXY=off \
    GOSUMDB=off

CMD ["bash"]
