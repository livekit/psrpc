package psrpc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"google.golang.org/protobuf/proto"
)

const lockExpiration = time.Second * 5

type redisMessageBus struct {
	rc redis.UniversalClient
}

func NewRedisMessageBus(rc redis.UniversalClient) MessageBus {
	return &redisMessageBus{rc: rc}
}

func (r *redisMessageBus) Publish(ctx context.Context, channel string, msg proto.Message) error {
	b, err := serialize(msg)
	if err != nil {
		return err
	}

	return r.rc.Publish(ctx, channel, b).Err()
}

func (r *redisMessageBus) Subscribe(ctx context.Context, channel string) (Subscription, error) {
	sub := r.rc.Subscribe(ctx, channel)
	msgChan := sub.Channel()
	dataChan := make(chan proto.Message, 100)
	go func() {
		for {
			msg, ok := <-msgChan
			if !ok {
				close(dataChan)
				return
			}

			p, err := deserialize([]byte(msg.Payload))
			if err != nil {
				// TODO: logger
				fmt.Println(err)
				continue
			}
			dataChan <- p
		}
	}()

	return &redisSubscription{
		sub: sub,
		c:   dataChan,
	}, nil
}

func (r *redisMessageBus) SubscribeQueue(ctx context.Context, channel string) (Subscription, error) {
	sub := r.rc.Subscribe(ctx, channel)
	msgChan := sub.Channel()
	dataChan := make(chan proto.Message, 100)
	go func() {
		for {
			msg, ok := <-msgChan
			if !ok {
				close(dataChan)
				return
			}

			sha := sha256.Sum256([]byte(msg.Payload))
			hash := base64.StdEncoding.EncodeToString(sha[:])
			acquired, _ := r.rc.SetNX(ctx, hash, rand.Int(), lockExpiration).Result()
			if acquired {
				p, err := deserialize([]byte(msg.Payload))
				if err != nil {
					// TODO: logger
					fmt.Println(err)
					continue
				}
				dataChan <- p
			}
		}
	}()

	return &redisSubscription{
		sub: sub,
		c:   dataChan,
	}, nil
}

type redisSubscription struct {
	sub *redis.PubSub
	c   <-chan proto.Message
}

func (r *redisSubscription) Channel() <-chan proto.Message {
	return r.c
}

func (r *redisSubscription) Close() error {
	return r.sub.Close()
}
