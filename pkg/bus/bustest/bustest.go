// Copyright 2025 LiveKit, Inc.
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

// Package bustest is the psrpc bus conformance harness.
package bustest

import (
	"testing"

	"github.com/livekit/psrpc/pkg/bus"
)

var servers []serverInfo

type serverInfo struct {
	Name string
	Func ServerFunc
}

// Brings up a broker for the duration of the test.
type ServerFunc func(t testing.TB) Server

// Registration is per binary, so call this from an init function in a package
// the test binary imports.
func RegisterServer(name string, fnc ServerFunc) {
	servers = append(servers, serverInfo{
		Name: name,
		Func: fnc,
	})
}

type Server interface {
	// Repeated calls must return separate peers that can reach each other.
	Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus
}

type Connect func(t testing.TB, opts ...bus.BusOption) bus.MessageBus

func TestAll(t *testing.T, test func(t *testing.T, bus Connect)) {
	for _, c := range servers {
		t.Run(c.Name, func(t *testing.T) {
			s := c.Func(t)
			test(t, s.Connect)
		})
	}
}
