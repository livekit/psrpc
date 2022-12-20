package psrpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"google.golang.org/protobuf/proto"
)

const lockExpiration = time.Second * 5

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	return &bus{
		busType: redisBus,
		rc:      rc,
	}
}

func redisPublish(rc redis.UniversalClient, ctx context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return rc.Publish(ctx, channel, b).Err()
}

func redisSubscribe[MessageType proto.Message](
	rc redis.UniversalClient, ctx context.Context, channel string,
) (Subscription[MessageType], error) {

	sub := rc.Subscribe(ctx, channel)
	msgChan := sub.Channel()
	dataChan := make(chan MessageType, ChannelSize)
	go func() {
		for {
			msg, ok := <-msgChan
			if !ok {
				close(dataChan)
				return
			}

			p, err := deserialize([]byte(msg.Payload))
			if err != nil {
				logger.Error(err, "failed to deserialize message")
				continue
			}
			dataChan <- p.(MessageType)
		}
	}()

	return &redisSubscription[MessageType]{
		sub: sub,
		c:   dataChan,
	}, nil
}

func redisSubscribeQueue[MessageType proto.Message](
	rc redis.UniversalClient, ctx context.Context, channel string,
) (Subscription[MessageType], error) {

	sub := rc.Subscribe(ctx, channel)
	msgChan := sub.Channel()
	dataChan := make(chan MessageType, ChannelSize)
	go func() {
		for {
			msg, ok := <-msgChan
			if !ok {
				close(dataChan)
				return
			}

			sha := sha256.Sum256([]byte(msg.Payload))
			hash := base64.StdEncoding.EncodeToString(sha[:])
			acquired, err := rc.SetNX(ctx, hash, rand.Int(), lockExpiration).Result()
			if err != nil {
				logger.Error(err, "failed to acquire redis lock")
				continue
			} else if acquired {
				p, err := deserialize([]byte(msg.Payload))
				if err != nil {
					logger.Error(err, "failed to deserialize message")
					continue
				}
				dataChan <- p.(MessageType)
			}
		}
	}()

	return &redisSubscription[MessageType]{
		sub: sub,
		c:   dataChan,
	}, nil
}

type redisSubscription[MessageType proto.Message] struct {
	sub *redis.PubSub
	c   <-chan MessageType
}

func (r *redisSubscription[MessageType]) Channel() <-chan MessageType {
	return r.c
}

func (r *redisSubscription[MessageType]) Close() error {
	return r.sub.Close()
}
