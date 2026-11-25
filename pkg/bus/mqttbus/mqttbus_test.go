// Copyright 2025 LiveKit, Inc.
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

package mqttbus

import (
	"context"
	"testing"

	"github.com/livekit/psrpc/pkg/bus"
)

func TestTopicMapping(t *testing.T) {
	for _, c := range []struct {
		subject string
		topic   string
	}{
		{"SRV.service", "SRV/service"},
		{"SRV.service.topic", "SRV/service/topic"},
		{"SRV.service.topic.Q", "SRV/service/topic/Q"},
		{"CLI.service.client-id.RES", "CLI/service/client-id/RES"},
	} {
		if got := topicName(c.subject); got != c.topic {
			t.Errorf("topicName(%q) = %q, want %q", c.subject, got, c.topic)
		}
	}
}

func TestQueueFilter(t *testing.T) {
	if got := queueFilter("SRV/service/topic"); got != "$share/psrpc/SRV/service/topic" {
		t.Errorf("queueFilter = %q", got)
	}
}

// Closed-bus Publish must fail without touching a connection.
func TestPublishClosed(t *testing.T) {
	connCtx, cancel := context.WithCancel(context.Background())
	b := &transport{
		subs:      map[string]*subList{},
		queues:    map[string]*subList{},
		routers:   map[string]*router{},
		connCtx:   connCtx,
		connCancel: cancel,
	}
	_ = b.Close()
	err := b.Publish(t.Context(), bus.Channel{Server: "SRV.s"}, []byte("x"))
	if err == nil {
		t.Fatal("Publish after Close must fail")
	}
}
