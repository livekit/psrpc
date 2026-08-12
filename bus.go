// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package psrpc

import (
	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"

	"github.com/livekit/psrpc/internal/bus"
)

type Channel = bus.Channel
type MessageBus bus.MessageBus

// ExclusiveQueuer is implemented by a MessageBus whose SubscribeQueue delivers
// each message to exactly one subscriber across every process. A bus that does
// not declare it keeps the conservative behavior; a bus that wraps another must
// forward this.
type ExclusiveQueuer = bus.ExclusiveQueuer

func NewLocalMessageBus() MessageBus {
	return bus.NewLocalMessageBus()
}

func NewNatsMessageBus(nc *nats.Conn) MessageBus {
	return bus.NewNatsMessageBus(nc)
}

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	return bus.NewRedisMessageBus(rc)
}
