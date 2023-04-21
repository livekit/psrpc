package logger

import "github.com/go-logr/logr"

var logger = logr.Discard()

func SetLogger(l logr.Logger) {
	logger = l
}

func Error(err error, msg string, values ...interface{}) {
	logger.Error(err, msg, values...)
}
