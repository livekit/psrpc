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

func NewClientID() string {
	return formatID("CLI_")
}

func NewServerID() string {
	return formatID("SRV_")
}

func NewRequestID() string {
	return formatID("REQ_")
}

func NewStreamID() string {
	return formatID("STR_")
}

func NewString() string {
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
