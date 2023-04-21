package bus

import (
	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal/logger"
)

type Subscription[MessageType proto.Message] interface {
	Channel() <-chan MessageType
	Close() error
}

type subscription[MessageType proto.Message] struct {
	Reader
	c <-chan MessageType
}

func newSubscription[MessageType proto.Message](sub Reader, size int) Subscription[MessageType] {
	msgChan := make(chan MessageType, size)
	go func() {
		for {
			b, ok := sub.read()
			if !ok {
				close(msgChan)
				return
			}

			p, err := deserialize(b)
			if err != nil {
				logger.Error(err, "failed to deserialize message")
				continue
			}
			msgChan <- p.(MessageType)
		}
	}()

	return &subscription[MessageType]{
		Reader: sub,
		c:      msgChan,
	}
}

func (s *subscription[MessageType]) Channel() <-chan MessageType {
	return s.c
}
