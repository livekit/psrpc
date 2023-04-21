package client

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
	"github.com/livekit/psrpc/internal/logger"
	"github.com/livekit/psrpc/internal/rand"
	"github.com/livekit/psrpc/internal/streams"
	"github.com/livekit/psrpc/pkg/info"
	"github.com/livekit/psrpc/pkg/metadata"
)

func OpenStream[SendType, RecvType proto.Message](
	ctx context.Context,
	c *RPCClient,
	rpc string,
	topic []string,
	opts ...psrpc.RequestOption,
) (psrpc.ClientStream[SendType, RecvType], error) {

	i := c.GetInfo(rpc, topic)
	o := getRequestOpts(i, c.ClientOpts, opts...)

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
	stream := streams.NewStream[SendType, RecvType](
		ctx,
		i,
		streamID,
		c.Timeout,
		&clientStream{c: c, i: i},
		c.StreamInterceptors,
		make(chan RecvType, c.ChannelSize),
		map[string]chan struct{}{requestID: ackChan},
	)

	go runClientStream(c, stream, recvChan)

	octx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()

	if err := c.bus.Publish(octx, i.GetStreamServerChannel(), req); err != nil {
		_ = stream.Close(err)
		return nil, psrpc.NewError(psrpc.Internal, err)
	}

	if i.RequireClaim {
		serverID, err := selectServer(octx, claimChan, nil, o.SelectionOpts)
		if err != nil {
			_ = stream.Close(err)
			return nil, err
		}
		if err = c.bus.Publish(octx, i.GetClaimResponseChannel(), &internal.ClaimResponse{
			RequestId: requestID,
			ServerId:  serverID,
		}); err != nil {
			_ = stream.Close(err)
			return nil, psrpc.NewError(psrpc.Internal, err)
		}
	}

	select {
	case <-ackChan:
		return stream, nil

	case <-octx.Done():
		err := octx.Err()
		if errors.Is(err, context.Canceled) {
			err = psrpc.ErrRequestCanceled
		} else if errors.Is(err, context.DeadlineExceeded) {
			err = psrpc.ErrRequestTimedOut
		}
		_ = stream.Close(err)
		return nil, err
	}
}

func runClientStream[SendType, RecvType proto.Message](
	c *RPCClient,
	stream streams.Stream[SendType, RecvType],
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
