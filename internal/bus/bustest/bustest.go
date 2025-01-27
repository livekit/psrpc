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

package bustest

import (
	"math/rand/v2"
	"net"
	"testing"

	"github.com/ory/dockertest/v3"

	"github.com/livekit/psrpc/internal/bus"
)

var (
	baseID  = rand.Uint32N(1000)
	servers []serverInfo
)

type serverInfo struct {
	Name string
	Func ServerFunc
}

type ServerFunc func(t testing.TB, pool *dockertest.Pool) Server

func RegisterServer(name string, fnc ServerFunc) {
	servers = append(servers, serverInfo{
		Name: name,
		Func: fnc,
	})
}

func waitTCPPort(t testing.TB, pool *dockertest.Pool, addr string) {
	if err := pool.Retry(func() error {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Log(err)
			return err
		}
		_ = conn.Close()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func Docker(t testing.TB) *dockertest.Pool {
	pool, err := dockertest.NewPool("")
	if err != nil {
		t.Fatal(err)
	}
	err = pool.Client.Ping()
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

type Server interface {
	Connect(t testing.TB) bus.MessageBus
}

func TestAll(t *testing.T, test func(t *testing.T, bus func(t testing.TB) bus.MessageBus)) {
	pool := Docker(t)
	for _, c := range servers {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			s := c.Func(t, pool)
			test(t, s.Connect)
		})
	}
}
