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

package test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twitchtv/twirp"

	"github.com/livekit/psrpc"
)

func TestAs(t *testing.T) {
	err := psrpc.NewErrorf(psrpc.NotFound, "test error")

	var twErr twirp.Error
	var psrpcErr psrpc.Error

	ret := errors.As(err, &twErr)
	assert.True(t, ret)
	assert.Equal(t, twirp.NotFound, twErr.Code())

	ret = errors.As(err, &psrpcErr)
	assert.True(t, ret)
	assert.Equal(t, err, psrpcErr)
}
