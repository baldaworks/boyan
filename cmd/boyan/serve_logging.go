package main

import (
	"fmt"
	stdhttp "net/http"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

func newLogger(cfg logConfig) (zerolog.Logger, error) {
	level := zerolog.InfoLevel
	if strings.TrimSpace(cfg.Level) != "" {
		parsed, err := zerolog.ParseLevel(strings.TrimSpace(cfg.Level))
		if err != nil {
			return zerolog.Logger{}, fmt.Errorf("parse Boyan log level: %w", err)
		}
		level = parsed
	}
	logger := zerolog.New(os.Stderr).With().Timestamp().Str("service", "boyan").Logger().Level(level)
	return logger, nil
}

func loggingMiddleware(logger zerolog.Logger, next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(writer stdhttp.ResponseWriter, request *stdhttp.Request) {
		started := time.Now().UTC()
		recorder := &statusRecorder{ResponseWriter: writer, status: stdhttp.StatusOK}
		next.ServeHTTP(recorder, request)
		logger.Info().
			Str("method", request.Method).
			Str("path", request.URL.Path).
			Int("status", recorder.status).
			Dur("elapsed", time.Since(started)).
			Msg("Boyan request")
	})
}

type statusRecorder struct {
	stdhttp.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
