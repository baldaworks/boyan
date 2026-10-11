package httpingest

import (
	"context"
	"io"
	stdhttp "net/http"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func livezHandler(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.Method != stdhttp.MethodGet {
		writeSafeError(
			w,
			r,
			stdhttp.StatusMethodNotAllowed,
			memory.PermanentError(memory.CodePermanent, "method not allowed", nil),
			nil,
		)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

func readyzHandler(ready ReadyChecker, timeout time.Duration) stdhttp.HandlerFunc {
	return func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		if r.Method != stdhttp.MethodGet {
			writeSafeError(
				w,
				r,
				stdhttp.StatusMethodNotAllowed,
				memory.PermanentError(memory.CodePermanent, "method not allowed", nil),
				nil,
			)

			return
		}

		if ready != nil {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			if err := ready.CheckReadiness(ctx); err != nil {
				writeSafeError(w, r, stdhttp.StatusServiceUnavailable, err, nil)

				return
			}
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = io.WriteString(w, "ok\n")
	}
}
