package bustest

import (
	"os"
	"testing"

	"github.com/ory/dockertest/v4"

	"github.com/livekit/psrpc/internal/bus"
)

func init() {
	RegisterServer("RabbitMQ", NewRabbitMQ)
}

// NewRabbitMQ connects to an external RabbitMQ instance pointed to by AMQP_URL
// (e.g. amqp://guest:guest@localhost:5672/ or amqps://user:pass@host/vhost).
// The suite skips this server when AMQP_URL is not set.
func NewRabbitMQ(t testing.TB, _ dockertest.Pool) Server {
	url := os.Getenv("AMQP_URL")
	if url == "" {
		t.Skip("AMQP_URL not set; skipping RabbitMQ conformance tests")
	}
	return &rabbitMQServer{url: url}
}

type rabbitMQServer struct {
	url string
}

func (s *rabbitMQServer) Connect(t testing.TB) bus.MessageBus {
	b, err := bus.NewAmqpMessageBus(s.url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
	})
	return b
}
