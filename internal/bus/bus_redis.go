package bus

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"math/rand"
	"time"

	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/proto"
)

const lockExpiration = time.Second * 5

type redisMessageBus struct {
	rc redis.UniversalClient
}

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	return &redisMessageBus{
		rc: rc,
	}
}

func (r *redisMessageBus) Publish(ctx context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return r.rc.Publish(ctx, channel, b).Err()
}

func (r *redisMessageBus) Subscribe(ctx context.Context, channel string, size int) (Reader, error) {
	sub := r.rc.Subscribe(ctx, channel)
	return &redisSubscription{
		sub:     sub,
		msgChan: sub.Channel(redis.WithChannelSize(size)),
	}, nil
}

func (r *redisMessageBus) SubscribeQueue(ctx context.Context, channel string, size int) (Reader, error) {
	sub := r.rc.Subscribe(ctx, channel)
	return &redisSubscription{
		ctx:     ctx,
		rc:      r.rc,
		sub:     sub,
		msgChan: sub.Channel(redis.WithChannelSize(size)),
		queue:   true,
	}, nil
}

type redisSubscription struct {
	ctx     context.Context
	rc      redis.UniversalClient
	sub     *redis.PubSub
	msgChan <-chan *redis.Message
	queue   bool
}

func (r *redisSubscription) read() ([]byte, bool) {
	for {
		msg, ok := <-r.msgChan
		if !ok {
			return nil, false
		}

		if r.queue {
			sha := sha256.Sum256([]byte(msg.Payload))
			hash := base64.StdEncoding.EncodeToString(sha[:])
			acquired, err := r.rc.SetNX(r.ctx, hash, rand.Int(), lockExpiration).Result()
			if err != nil || !acquired {
				continue
			}
		}

		return []byte(msg.Payload), true
	}
}

func (r *redisSubscription) Close() error {
	return r.sub.Close()
}
