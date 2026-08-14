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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/livekit/psrpc"
	"github.com/livekit/psrpc/internal"
)

func TestAffinity(t *testing.T) {
	testAffinity(t, psrpc.SelectionOpts{
		AcceptFirstAvailable: true,
	}, "1")

	testAffinity(t, psrpc.SelectionOpts{
		AcceptFirstAvailable: true,
		MinimumAffinity:      0.5,
	}, "2")

	testAffinity(t, psrpc.SelectionOpts{
		ShortCircuitTimeout: time.Millisecond * 150,
	}, "2")

	testAffinity(t, psrpc.SelectionOpts{
		MinimumAffinity:     0.4,
		AffinityTimeout:     0,
		ShortCircuitTimeout: time.Millisecond * 250,
	}, "3")

	testAffinity(t, psrpc.SelectionOpts{
		MinimumAffinity:     0.3,
		AffinityTimeout:     time.Millisecond * 250,
		ShortCircuitTimeout: time.Millisecond * 200,
	}, "2")

	testAffinity(t, psrpc.SelectionOpts{
		AffinityTimeout: time.Millisecond * 600,
	}, "5")
}

func testAffinity(t *testing.T, opts psrpc.SelectionOpts, expectedID string) {
	c := make(chan *internal.ClaimRequest, 100)
	go func() {
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "1",
			Affinity:  0.1,
		}
		time.Sleep(time.Millisecond * 100)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "2",
			Affinity:  0.5,
		}
		time.Sleep(time.Millisecond * 200)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "3",
			Affinity:  0.7,
		}
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "4",
			Affinity:  0.1,
		}
		time.Sleep(time.Millisecond * 200)
		c <- &internal.ClaimRequest{
			RequestId: "1",
			ServerId:  "5",
			Affinity:  0.9,
		}
	}()
	sel, err := selectServer(context.Background(), c, nil, opts)
	require.NoError(t, err)
	require.Equal(t, expectedID, sel.serverID)
}

// A bid from a server predating skip_claim must still be granted.
func TestSelectServerGrantsABid(t *testing.T) {
	claims := make(chan *internal.ClaimRequest, 1)
	claims <- &internal.ClaimRequest{RequestId: "1", ServerId: "2", Affinity: 1}

	sel, err := selectServer(context.Background(), claims, make(chan *internal.Response, 1),
		psrpc.SelectionOpts{AcceptFirstAvailable: true})
	require.NoError(t, err)
	require.Equal(t, "2", sel.serverID)
	require.False(t, sel.handling, "a bid still needs granting")
	require.Nil(t, sel.res)
}

// An announcement ends selection without a grant, whatever the affinity options
// would otherwise have done.
func TestSelectServerHonorsAnnouncement(t *testing.T) {
	claims := make(chan *internal.ClaimRequest, 1)
	claims <- &internal.ClaimRequest{RequestId: "1", ServerId: "2", Affinity: 1, Handling: true}

	sel, err := selectServer(context.Background(), claims, make(chan *internal.Response, 1),
		psrpc.SelectionOpts{MinimumAffinity: 2, AffinityTimeout: time.Second})
	require.NoError(t, err)
	require.Equal(t, "2", sel.serverID)
	require.True(t, sel.handling)
	require.Nil(t, sel.res)
}

// A response can beat the announcement, and must be handed back rather than
// consumed during selection.
func TestSelectServerReturnsEarlyResponse(t *testing.T) {
	responses := make(chan *internal.Response, 1)
	responses <- &internal.Response{RequestId: "1", ServerId: "2"}

	sel, err := selectServer(context.Background(), make(chan *internal.ClaimRequest, 1), responses,
		psrpc.SelectionOpts{AcceptFirstAvailable: true})
	require.NoError(t, err)
	require.NotNil(t, sel.res)
	require.Equal(t, "2", sel.res.ServerId)
	require.Empty(t, sel.serverID)
}
