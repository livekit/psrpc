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
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/internal/stream"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/metadata"
	"github.com/livekit/psrpc/pkg/rand"
)

func OpenStream[SendType, RecvType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
	opts ...psrpc.RequestOption,
) (psrpc.ClientStream[SendType, RecvType], error) {

	i := c.GetInfo(rpc, topic)
	o := getRequestOpts(ctx, i, c.ClientOpts, opts...)

	streamID := rand.NewStreamID()
	requestID := rand.NewRequestID()
	now := time.Now()
	req := &internal.Stream{
		StreamId:  streamID,
		RequestId: requestID,
		SentAt:    now.UnixNano(),
		Expiry:    now.Add(o.Timeout).UnixNano(),
		Body: &internal.Stream_Open{
			Open: &internal.StreamOpen{
				NodeId:   c.ID,
				Metadata: metadata.OutgoingContextMetadata(ctx),
			},
		},
	}

	claimChan := make(chan *internal.ClaimRequest, c.ChannelSize)
	recvChan := make(chan *internal.Stream, c.ChannelSize)

	c.mu.Lock()
	c.claimRequests[requestID] = claimChan
	c.streamChannels[streamID] = recvChan
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.claimRequests, requestID)
		c.mu.Unlock()
	}()

	ackChan := make(chan struct{})
	cs := stream.NewStream[SendType, RecvType](
		ctx,
		i,
		streamID,
		o.Timeout,
		&clientStream{c: c, i: i},
		getRequestInterceptors(c.StreamInterceptors, o.Interceptors),
		make(chan RecvType, c.ChannelSize),
		map[string]chan struct{}{requestID: ackChan},
	)

	go runClientStream(c, cs, recvChan)

	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	if err := c.bus.Publish(ctx, i.GetStreamServerChannel(), req); err != nil {
		_ = cs.Close(err)
		return nil, psrpc.NewError(psrpc.Internal, err)
	}

	if i.RequireClaim {
		serverID, err := selectServer(ctx, claimChan, nil, o.SelectionOpts)
		if err != nil {
			_ = cs.Close(err)
			return nil, err
		}

		if err = c.bus.Publish(ctx, i.GetClaimResponseChannel(), &internal.ClaimResponse{
			RequestId: requestID,
			ServerId:  serverID,
		}); err != nil {
			_ = cs.Close(err)
			return nil, psrpc.NewError(psrpc.Internal, err)
		}
	}

	select {
	case <-ackChan:
		return cs, nil

	case <-ctx.Done():
		err := ctx.Err()
		if errors.Is(err, context.Canceled) {
			err = psrpc.ErrRequestCanceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			err = psrpc.ErrRequestTimedOut
		}
		_ = cs.Close(err)
		return nil, err
	}
}

func runClientStream[SendType, RecvType proto.Message](
	c *RPCClient,
	stream stream.Stream[SendType, RecvType],
	recvChan chan *internal.Stream,
) {
	ctx := stream.Context()
	closed := c.closed.Watch()

	for {
		select {
		case <-ctx.Done():
			_ = stream.Close(ctx.Err())
			return

		case <-closed:
			_ = stream.Close(nil)
			return

		case is := <-recvChan:
			if time.Now().UnixNano() < is.Expiry {
				if err := stream.HandleStream(is); err != nil {
					logger.Error(err, "failed to handle request", "requestID", is.RequestId)
				}
			}
		}
	}
}

type clientStream struct {
	c *RPCClient
	i *info.RequestInfo
}

func (s *clientStream) Send(ctx context.Context, msg *internal.Stream) (err error) {
	if err = s.c.bus.Publish(ctx, s.i.GetStreamServerChannel(), msg); err != nil {
		err = psrpc.NewError(psrpc.Internal, err)
	}
	return
}

func (s *clientStream) Close(streamID string) {
	s.c.mu.Lock()
	delete(s.c.streamChannels, streamID)
	s.c.mu.Unlock()
}
