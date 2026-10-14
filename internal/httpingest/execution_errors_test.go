package httpingest_test

import (
	"errors"
	"net/http"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type executionErrorCase struct {
	name    string
	code    boyan.ErrorCode
	class   boyan.ErrorClass
	status  int
	message string
}

func executionErrorCases() []executionErrorCase {
	return []executionErrorCase{
		{"unavailable", boyan.CodeUnavailable, boyan.ErrorClassRetryable, 503, "ingest service is temporarily unavailable"},
		{"shutdown", boyan.CodeShuttingDown, boyan.ErrorClassRetryable, 503, "ingest service is shutting down"},
		{"disabled", boyan.CodeDisabled, boyan.ErrorClassPermanent, 503, "ingest service is temporarily unavailable"},
		{"store", boyan.CodeStoreFailure, boyan.ErrorClassRetryable, 503, "ingest service is temporarily unavailable"},
		{"model", boyan.CodeModelFailure, boyan.ErrorClassRetryable, 503, "ingest service is temporarily unavailable"},
		{"permanent", boyan.CodePermanent, boyan.ErrorClassPermanent, 503, "ingest service is temporarily unavailable"},
		{"timeout", boyan.CodeTimeout, boyan.ErrorClassRetryable, 504, "ingest request timed out"},
		{
			"retryable_limit", boyan.CodeLimitExceeded, boyan.ErrorClassRetryable, 429,
			"ingest service is temporarily at capacity",
		},
		{"permanent_limit", boyan.CodeLimitExceeded, boyan.ErrorClassPermanent, 400, "ingest request is invalid"},
		{"conflict", boyan.CodeConflict, boyan.ErrorClassRetryable, 409, "ingest request conflicted with durable state"},
		{"unknown", boyan.CodePermanent, boyan.ErrorClassPermanent, 500, "ingest request failed"},
	}
}

func TestHandlerExecutionErrorsPreserveStatuses(t *testing.T) {
	t.Parallel()

	for _, route := range []string{"turns", "boundaries"} {
		for _, tc := range executionErrorCases() {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				cause := errors.New(sensitiveMarker)

				var backendErr error

				switch {
				case tc.name == "unknown":
					backendErr = cause
				case tc.class == boyan.ErrorClassRetryable:
					backendErr = boyan.RetryableError(tc.code, sensitiveMarker, cause)
				default:
					backendErr = boyan.PermanentError(tc.code, sensitiveMarker, cause)
				}

				counter := &countingIngestor{err: backendErr}
				rec := serveIngestRequest(t, route, validRequestJSON(t, route), counter)
				assertHTTPError(t, rec, tc.status, tc.code, tc.class, tc.message)

				wantTurns, wantBoundaries := int64(1), int64(0)
				if route == "boundaries" {
					wantTurns, wantBoundaries = 0, 1
				}

				if counter.turns.Load() != wantTurns || counter.boundaries.Load() != wantBoundaries {
					t.Fatal("valid request did not reach only its selected ingestor once")
				}
			})
		}
	}
}

func TestHandlerStrictJSONRejectsBeforeIngestion(t *testing.T) {
	t.Parallel()

	for _, route := range []string{"turns", "boundaries"} {
		valid := validRequestJSON(t, route)

		tests := map[string]string{
			"empty":              "",
			"malformed":          "{\"" + sensitiveMarker + "\":",
			"unknown":            "{\"" + sensitiveMarker + "\":true," + valid[1:],
			"trailing":           valid + " {}",
			"trailing_malformed": valid + " " + sensitiveMarker,
		}
		for name, body := range tests {
			t.Run(route+"/"+name, func(t *testing.T) {
				counter := &countingIngestor{}
				rec := serveIngestRequest(t, route, body, counter)
				assertHTTPError(
					t, rec, http.StatusBadRequest, boyan.CodeInvalidDerived,
					boyan.ErrorClassPermanent, "ingest request is invalid",
				)

				if counter.turns.Load() != 0 || counter.boundaries.Load() != 0 {
					t.Fatal("invalid JSON reached ingestion")
				}
			})
		}
	}
}
