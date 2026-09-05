package bustest

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("Local", func(t testing.TB, pool dockertest.Pool) Server {
		return NewLocalBus()
	})
}

func NewLocalBus() Server {
	return &localBus{}
}

type localBus struct {
	mu  sync.Mutex
	bus map[string]bus.MessageBus
}

// Peers must share one instance to reach each other, so buses are keyed by the
// option set rather than created per call.
func (s *localBus) Connect(t testing.TB, opts ...bus.BusOption) bus.MessageBus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var o bus.BusOpts
	for _, opt := range opts {
		opt(&o)
	}
	k := fmt.Sprint(o)

	if s.bus == nil {
		s.bus = map[string]bus.MessageBus{}
	}
	if b, ok := s.bus[k]; ok {
		return b
	}
	b := bus.NewLocalMessageBus(opts...)
	s.bus[k] = b
	return b
}
