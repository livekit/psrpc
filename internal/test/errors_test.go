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
