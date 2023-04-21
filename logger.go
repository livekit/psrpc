package psrpc

import (
	"github.com/go-logr/logr"

	"github.com/livekit/psrpc/internal/logger"
)

func SetLogger(l logr.Logger) {
	logger.SetLogger(l)
}
