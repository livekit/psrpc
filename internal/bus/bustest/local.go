package bustest

import (
	"testing"

	"github.com/ory/dockertest/v3"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("Local", func(t testing.TB, pool *dockertest.Pool) Server {
		return NewLocalBus()
	})
}

func NewLocalBus() Server {
	b := bus.NewLocalMessageBus()
	return &localBus{b: b}
}

type localBus struct {
	b bus.MessageBus
}

func (s *localBus) Connect(t testing.TB) bus.MessageBus {
	return s.b
}
