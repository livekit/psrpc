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

package middleware

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	"github.com/livekit/psrpc"
)

// recvMetricsObserver records the arguments passed to OnStreamRecv. The
// embedded MetricsObserver is left nil on purpose: only OnStreamOpen (called
// when the interceptor wraps the handler) and OnStreamRecv are exercised here.
type recvMetricsObserver struct {
	MetricsObserver
	recvCalls int
	recvErr   error
	recvBytes int
}

func (o *recvMetricsObserver) OnStreamOpen(MetricRole, psrpc.RPCInfo) {}

func (o *recvMetricsObserver) OnStreamRecv(_ MetricRole, _ psrpc.RPCInfo, err error, bytes int) {
	o.recvCalls++
	o.recvErr = err
	o.recvBytes = bytes
}

// recvStreamHandler is a fake downstream handler whose Recv populates the
// message and returns a configurable error, so the test can tell whether the
// metric was recorded before or after the handler ran.
type recvStreamHandler struct {
	psrpc.StreamHandler
	err  error
	fill string
}

func (h *recvStreamHandler) Recv(msg proto.Message) error {
	if sv, ok := msg.(*wrapperspb.StringValue); ok {
		sv.Value = h.fill
	}
	return h.err
}

// TestStreamMetricsRecvObservedAfterHandler guards against recording the recv
// metric before the downstream handler runs. If OnStreamRecv fires first, the
// error is always nil and the byte count is measured on an empty message,
// making recv-side error rates and throughput unobservable. The observation
// must happen after Recv so it reflects the returned error and populated msg.
func TestStreamMetricsRecvObservedAfterHandler(t *testing.T) {
	sentinel := errors.New("recv failed")
	obs := &recvMetricsObserver{}
	next := &recvStreamHandler{err: sentinel, fill: "hello world"}

	h := newStreamMetricsInterceptor(obs, ServerRole)(psrpc.RPCInfo{}, next)

	msg := &wrapperspb.StringValue{}
	err := h.Recv(msg)

	require.ErrorIs(t, err, sentinel)
	require.Equal(t, 1, obs.recvCalls)
	require.ErrorIs(t, obs.recvErr, sentinel, "OnStreamRecv must observe the error returned by the handler")
	require.NotZero(t, obs.recvBytes, "OnStreamRecv must observe the size of the populated message, not the empty one")
	require.Equal(t, proto.Size(msg), obs.recvBytes, "OnStreamRecv byte count must match the populated message")
}
