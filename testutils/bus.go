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

package testutils

import (
	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal/bus"
)

type PublishInterceptor = bus.PublishInterceptor
type SubscribeInterceptor = bus.SubscribeInterceptor

type PublishHandler = bus.PublishHandler
type ReadHandler = bus.ReadHandler

type TestBusOption = bus.TestBusOption

func WithPublishInterceptor(interceptor PublishInterceptor) TestBusOption {
	return func(o *bus.TestBusOpts) {
		o.PublishInterceptors = append(o.PublishInterceptors, interceptor)
	}
}

func WithSubscribeInterceptor(interceptor SubscribeInterceptor) TestBusOption {
	return func(o *bus.TestBusOpts) {
		o.SubscribeInterceptors = append(o.SubscribeInterceptors, interceptor)
	}
}

func NewTestBus(next psrpc.MessageBus, opts ...TestBusOption) psrpc.MessageBus {
	return bus.NewTestBus(next, opts...)
}
