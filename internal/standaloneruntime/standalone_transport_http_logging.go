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

	status      int
	wroteHeader bool
}

func (r *statusRecorder) Unwrap() stdhttp.ResponseWriter {
	return r.ResponseWriter
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}

	if status >= stdhttp.StatusOK {
		r.status = status
		r.wroteHeader = true
	}

	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(body []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(stdhttp.StatusOK)
	}

	return r.ResponseWriter.Write(body)
}

func (r *statusRecorder) FlushError() error {
	if err := stdhttp.NewResponseController(r.ResponseWriter).Flush(); err != nil {
		return err
	}

	if !r.wroteHeader {
		r.status = stdhttp.StatusOK
		r.wroteHeader = true
	}

	return nil
}
