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

// Package dockerutil holds the container plumbing shared by the broker-backed
// bustest servers. Kept out of bustest so the conformance harness itself
// carries no dockertest dependency.
package dockerutil

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/ory/dockertest/v4"
)

var (
	pool = sync.OnceValues(func() (dockertest.Pool, error) {
		return dockertest.NewPool(context.Background(), "")
	})

	// Seeded randomly so concurrent test binaries don't collide on names.
	nameSeq = newNameSeq()
)

func newNameSeq() *atomic.Uint32 {
	var n atomic.Uint32
	n.Store(rand.Uint32N(1000))
	return &n
}

func Pool(t testing.TB) dockertest.Pool {
	p, err := pool()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func Name(prefix string) string {
	return fmt.Sprintf("psrpc-%s-%d", prefix, nameSeq.Add(1))
}

func WaitTCPPort(t testing.TB, pool dockertest.Pool, addr string) {
	if err := pool.Retry(context.Background(), 0, func() error {
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
