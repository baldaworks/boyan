package httpingest

import (
	"encoding/json"
	stdhttp "net/http"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func writeSafeError(
	w stdhttp.ResponseWriter,
	_ *stdhttp.Request,
	status int,
	err error,
	retryAfter *time.Duration,
) {
	code, class, ok := memory.ClassifyError(err)
	if !ok {
		code = memory.CodePermanent
		class = memory.ErrorClassPermanent
	}

	body := memory.IngestErrorResponse{
		Code:    code,
		Class:   class,
		Message: safeErrorMessage(code, status),
	}

	if retryAfter != nil {
		delay := retryAfter.Milliseconds()
		body.RetryAfterMS = &delay
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
