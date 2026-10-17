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

package amqpbus

import (
	"testing"
)

func TestExchangeName(t *testing.T) {
	for _, c := range []struct {
		subject string
		name    string
	}{
		{"SRV.service", "SRV.service"},
		{"CLI.service.client-id.RES", "CLI.service.client-id.RES"},
	} {
		if got := exchangeName(c.subject); got != c.name {
			t.Errorf("exchangeName(%q) = %q, want %q", c.subject, got, c.name)
		}
	}
}

// Over-long names must collapse to the deterministic hash alias, stable
// across calls and distinct between inputs.
func TestExchangeNameHashAlias(t *testing.T) {
	long := "SRV." + string(make([]byte, 300))
	a := exchangeName(long)
	b := exchangeName(long)
	if a != b {
		t.Errorf("alias not deterministic: %q vs %q", a, b)
	}
	if len(a) > amqpMaxNameLen {
		t.Errorf("alias exceeds shortstr limit: %d", len(a))
	}
	if a == exchangeName(long+"x") {
		t.Error("distinct inputs must not alias to the same name")
	}
}

func TestPubSlotIndexStable(t *testing.T) {
	if pubSlotIndex("SRV.s") != pubSlotIndex("SRV.s") {
		t.Error("slot index must be stable per name")
	}
}
