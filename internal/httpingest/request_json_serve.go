package httpingest

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func serveJSON(
	w stdhttp.ResponseWriter,
	r *stdhttp.Request,
	timeout time.Duration,
	maxBodyBytes int64,
	handle func(context.Context, []byte) (memory.IngestReceipt, error),
) {
	if r.Method != stdhttp.MethodPost {
		writeSafeError(
			w,
			r,
			stdhttp.StatusMethodNotAllowed,
			memory.PermanentError(memory.CodePermanent, "method not allowed", nil),
			nil,
		)

		return
	}

	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeSafeError(
			w,
			r,
			stdhttp.StatusBadRequest,
			memory.PermanentError(memory.CodeInvalidDerived, "content type must be application/json", nil),
			nil,
		)

		return
	}

	limited := stdhttp.MaxBytesReader(w, r.Body, maxBodyBytes)
	defer limited.Close()

	body, err := io.ReadAll(limited)
	if err != nil {
		limitErr := memory.PermanentError(memory.CodeLimitExceeded, "request body exceeds limit", err)
		writeSafeError(w, r, statusFromError(limitErr), limitErr, nil)

		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	receipt, err := handle(ctx, body)
	if err != nil {
		writeSafeError(w, r, statusFromError(err), err, nil)

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(stdhttp.StatusOK)
	_ = json.NewEncoder(w).Encode(receipt)
}
