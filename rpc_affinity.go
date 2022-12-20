package psrpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/livekit/psrpc/internal"
)

type AffinityFunc[RequestType proto.Message] func(RequestType) float32

type AffinityOpts struct {
	AcceptFirstAvailable bool
	MinimumAffinity      float32
	AffinityTimeout      time.Duration
	ShortCircuitTimeout  time.Duration
}

func selectServer(ctx context.Context, claimChan chan *internal.ClaimRequest, opts AffinityOpts) (string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	if opts.AffinityTimeout > 0 {
		time.AfterFunc(opts.AffinityTimeout, cancel)
	}

	serverID := ""
	best := float32(0)
	shorted := false

	for {
		select {
		case <-ctx.Done():
			if best == 0 {
				return "", errors.New("no valid servers found")
			} else {
				return serverID, nil
			}

		case claim := <-claimChan:
			if (opts.MinimumAffinity > 0 && claim.Affinity >= opts.MinimumAffinity && claim.Affinity > best) ||
				(opts.MinimumAffinity <= 0 && claim.Affinity > best) {
				if opts.AcceptFirstAvailable {
					return claim.ServerId, nil
				}

				serverID = claim.ServerId
				best = claim.Affinity

				if opts.ShortCircuitTimeout > 0 && !shorted {
					shorted = true
					time.AfterFunc(opts.ShortCircuitTimeout, cancel)
				}
			}
		}
	}
}
