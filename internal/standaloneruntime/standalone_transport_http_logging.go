package standaloneruntime

import (
	stdhttp "net/http"
	"time"

	"github.com/rs/zerolog"
)

func httpLoggingMiddleware(logger zerolog.Logger, next stdhttp.Handler) stdhttp.Handler {
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
