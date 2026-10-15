package standaloneruntime

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *standaloneService) beginOperation(ctx context.Context) (context.Context, func(), error) {
	return s.beginTrackedOperation(ctx, true)
}

func (s *standaloneService) beginHealthOperation(ctx context.Context) (context.Context, func(), error) {
	return s.beginTrackedOperation(ctx, false)
}

func (s *standaloneService) beginTrackedOperation(
	ctx context.Context,
	requireStarted bool,
) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closing {
		s.mu.Unlock()

		return nil, nil, memory.RetryableError(memory.CodeShuttingDown, "Boyan runtime is shutting down", nil)
	}

	if requireStarted && !s.started {
		s.mu.Unlock()

		return nil, nil, memory.RetryableError(memory.CodeUnavailable, "Boyan runtime is not started", nil)
	}

	s.operations.Add(1)
	s.mu.Unlock()

	operation, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.shutdownContext, cancel)

	return operation, func() { stop(); cancel(); s.operations.Done() }, nil
}

func (s *standaloneService) operationHandler(next stdhttp.Handler) stdhttp.Handler {
	return stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		begin := s.beginOperation
		if r.URL.Path == "/livez" || r.URL.Path == "/readyz" {
			begin = s.beginHealthOperation
		}

		if r.URL.Path == "/readyz" && s.semantic != nil {
			state := s.semantic.summary()
			w.Header().Set("X-Boyan-Semantic-State", state)

			mode := "degraded"
			if state == "hybrid-ready" {
				mode = "hybrid"
			} else {
				w.Header().Set("X-Boyan-Retrieval-Reason", "unavailable")
			}

			w.Header().Set("X-Boyan-Retrieval-Mode", mode)
		}

		ctx, done, err := begin(r.Context())
		if err != nil {
			code, class, _ := memory.ClassifyError(err)

			if strings.HasPrefix(r.URL.Path, "/v1/ingest/") {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(stdhttp.StatusServiceUnavailable)
				_ = json.NewEncoder(w).Encode(
					memory.IngestErrorResponse{
						Code:    code,
						Class:   class,
						Message: err.Error(),
					},
				)
			} else {
				stdhttp.Error(w, err.Error(), stdhttp.StatusServiceUnavailable)
			}

			return
		}
		defer done()

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
