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

package rand

import (
	"math/rand"
	"sync"
	"time"
)

const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var idRNG = rand.New(&lockedRandSource{src: rand.NewSource(time.Now().UnixNano())})

type lockedRandSource struct {
	mu  sync.Mutex
	src rand.Source
}

func (s *lockedRandSource) Int63() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.src.Int63()
}

func (s *lockedRandSource) Seed(seed int64) {
	s.mu.Lock()
	s.src.Seed(seed)
	s.mu.Unlock()
}

func NewRequestID() string {
	return formatID("REQ_")
}

func NewStreamID() string {
	return formatID("STR_")
}

func String() string {
	return formatID("")
}

func formatID(prefix string) string {
	b := make([]byte, len(prefix)+12)
	copy(b, prefix)
	readIDChars(b[len(prefix):])
	return string(b)
}

func readIDChars(b []byte) {
	var n int
	for {
		r := idRNG.Int63()
		for i := 0; i < 10; i++ {
			if int(r&0x3f) < len(alphabet) {
				b[n] = alphabet[r&0x3f]
				n++
				if n == len(b) {
					return
				}
			}
			r >>= 6
		}
	}
}
