package psrpc

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/twitchtv/twirp"
)

func TestAs(t *testing.T) {
	err := NewErrorf(NotFound, "test error")

	var twErr twirp.Error
	ret := errors.As(err, &twErr)
	assert.True(t, ret)
	assert.Equal(t, twirp.NotFound, twErr.Code())
}
