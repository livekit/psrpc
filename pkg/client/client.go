package client

import (
	"context"
	"sync"

	"github.com/frostbyte73/core"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/internal/channels"
)

type RPCClient struct {
	psrpc.ClientOpts

	bus              bus.MessageBus
	serviceName      string
	id               string
	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	streamChannels   map[string]chan *internal.Stream
	closed           core.Fuse
}

func NewRPCClientWithStreams(serviceName, clientID string, b bus.MessageBus, opts ...psrpc.ClientOption) (*RPCClient, error) {
	return NewRPCClient(serviceName, clientID, b, append(opts, withStreams())...)
}

func NewRPCClient(serviceName, clientID string, b bus.MessageBus, opts ...psrpc.ClientOption) (*RPCClient, error) {
	c := &RPCClient{
		ClientOpts:       getClientOpts(opts...),
		bus:              b,
		serviceName:      serviceName,
		id:               clientID,
		claimRequests:    make(map[string]chan *internal.ClaimRequest),
		responseChannels: make(map[string]chan *internal.Response),
		streamChannels:   make(map[string]chan *internal.Stream),
		closed:           core.NewFuse(),
	}

	ctx := context.Background()
	responses, err := bus.Subscribe[*internal.Response](
		ctx, c.bus, channels.ResponseChannel(serviceName, clientID), c.ChannelSize,
	)
	if err != nil {
		return nil, err
	}

	claims, err := bus.Subscribe[*internal.ClaimRequest](
		ctx, c.bus, channels.ClaimRequestChannel(serviceName, clientID), c.ChannelSize,
	)
	if err != nil {
		_ = responses.Close()
		return nil, err
	}

	var streams bus.Subscription[*internal.Stream]
	if c.EnableStreams {
		streams, err = bus.Subscribe[*internal.Stream](
			ctx, c.bus, channels.StreamChannel(serviceName, clientID), c.ChannelSize,
		)
		if err != nil {
			_ = responses.Close()
			_ = claims.Close()
			return nil, err
		}
	} else {
		streams = bus.EmptySubscription[*internal.Stream]{}
	}

	go func() {
		closed := c.closed.Watch()
		for {
			select {
			case <-closed:
				_ = claims.Close()
				_ = responses.Close()
				_ = streams.Close()
				return

			case claim := <-claims.Channel():
				c.mu.RLock()
				claimChan, ok := c.claimRequests[claim.RequestId]
				c.mu.RUnlock()
				if ok {
					claimChan <- claim
				}

			case res := <-responses.Channel():
				c.mu.RLock()
				resChan, ok := c.responseChannels[res.RequestId]
				c.mu.RUnlock()
				if ok {
					resChan <- res
				}

			case msg := <-streams.Channel():
				c.mu.RLock()
				streamChan, ok := c.streamChannels[msg.StreamId]
				c.mu.RUnlock()
				if ok {
					streamChan <- msg
				}

			}
		}
	}()

	return c, nil
}

func (c *RPCClient) Close() {
	c.closed.Break()
}
