package httpingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestWriteSafeErrorPreservesMarkedClassificationAndRetryMetadata(t *testing.T) {
	t.Parallel()

	cause := errors.New("secret-backend-cause")

	err := fmt.Errorf("outer context: %w", &requestValidationError{
		err: memory.PermanentError(memory.CodePermanent, "secret-validation-detail", cause),
	})
	if !errors.Is(err, cause) {
		t.Fatal("validation marker lost original cause")
	}

	for _, withRetry := range []bool{false, true} {
		t.Run(fmt.Sprint(withRetry), func(t *testing.T) {
			var retry *time.Duration

			if withRetry {
				delay := 1500 * time.Millisecond
				retry = &delay
			}

			rec := httptest.NewRecorder()
			writeSafeError(rec, httptest.NewRequest(http.MethodPost, "/", nil), statusFromError(err), err, retry)

			var body memory.IngestErrorResponse
			if decodeErr := json.Unmarshal(rec.Body.Bytes(), &body); decodeErr != nil {
				t.Fatalf("decode response: %v", decodeErr)
			}

			assertMarkedErrorBody(t, rec.Code, body)

			assertRetryMetadata(t, body.RetryAfterMS, withRetry)

			if strings.Contains(rec.Body.String(), "secret-") || strings.Contains(rec.Body.String(), "outer context") {
				t.Fatalf("response disclosed diagnostic text: %s", rec.Body.String())
			}
		})
	}
}

func assertRetryMetadata(t *testing.T, delay *int64, wantRetry bool) {
	t.Helper()

	if wantRetry {
		if delay == nil || *delay != 1500 {
			t.Fatalf("retry metadata = %v, want 1500ms", delay)
		}
	} else if delay != nil {
		t.Fatalf("unexpected retry metadata = %v", delay)
	}
}

func assertMarkedErrorBody(t *testing.T, status int, body memory.IngestErrorResponse) {
	t.Helper()

	if status != http.StatusBadRequest || body.Code != memory.CodePermanent ||
		body.Class != memory.ErrorClassPermanent || body.Message != "ingest request is invalid" {
		t.Fatalf("status = %d, body = %+v, want safe original classification with 400", status, body)
	}
}
