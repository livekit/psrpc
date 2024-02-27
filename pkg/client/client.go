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

package client

import (
	"context"
	"sync"

	"github.com/frostbyte73/core"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/bus"
	"github.com/livekit/psrpc/pkg/info"
)

type RPCClient struct {
	*info.ServiceDefinition
	psrpc.ClientOpts

	bus bus.MessageBus

	mu               sync.RWMutex
	claimRequests    map[string]chan *internal.ClaimRequest
	responseChannels map[string]chan *internal.Response
	streamChannels   map[string]chan *internal.Stream
	closed           core.Fuse
}

func NewRPCClientWithStreams(
	sd *info.ServiceDefinition,
	b bus.MessageBus,
	opts ...psrpc.ClientOption,
) (*RPCClient, error) {
	return NewRPCClient(sd, b, append(opts, withStreams())...)
}

func NewRPCClient(
	sd *info.ServiceDefinition,
	b bus.MessageBus,
	opts ...psrpc.ClientOption,
) (*RPCClient, error) {
	c := &RPCClient{
		ServiceDefinition: sd,
		ClientOpts:        getClientOpts(opts...),
		bus:               b,
		claimRequests:     make(map[string]chan *internal.ClaimRequest),
		responseChannels:  make(map[string]chan *internal.Response),
		streamChannels:    make(map[string]chan *internal.Stream),
	}
	if c.ClientID != "" {
		c.ID = c.ClientID
	}

	ctx := context.Background()
	responses, err := bus.Subscribe[*internal.Response](
		ctx, c.bus, info.GetResponseChannel(c.Name, c.ID), c.ChannelSize,
	)
	if err != nil {
		return nil, err
	}

	claims, err := bus.Subscribe[*internal.ClaimRequest](
		ctx, c.bus, info.GetClaimRequestChannel(c.Name, c.ID), c.ChannelSize,
	)
	if err != nil {
		_ = responses.Close()
		return nil, err
	}

	var streams bus.Subscription[*internal.Stream]
	if c.EnableStreams {
		streams, err = bus.Subscribe[*internal.Stream](
			ctx, c.bus, info.GetStreamChannel(c.Name, c.ID), c.ChannelSize,
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
				if claim == nil {
					c.Close()
					continue
				}
				c.mu.RLock()
				claimChan, ok := c.claimRequests[claim.RequestId]
				c.mu.RUnlock()
				if ok {
					claimChan <- claim
				}

			case res := <-responses.Channel():
				if res == nil {
					c.Close()
					continue
				}
				c.mu.RLock()
				resChan, ok := c.responseChannels[res.RequestId]
				c.mu.RUnlock()
				if ok {
					resChan <- res
				}

			case msg := <-streams.Channel():
				if msg == nil {
					c.Close()
					continue
				}
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
