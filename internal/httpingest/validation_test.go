package httpingest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	httpingest "github.com/baldaworks/boyan/internal/httpingest"
	boyan "github.com/baldaworks/boyan/pkg/memory"
)

const sensitiveMarker = "secret-payload-or-backend-detail"

type countingIngestor struct {
	turns      atomic.Int64
	boundaries atomic.Int64
	err        error
}

func (c *countingIngestor) IngestSequencedTurn(context.Context, boyan.SequencedTurn) (boyan.IngestOutcome, error) {
	c.turns.Add(1)

	return boyan.IngestOutcome{}, c.err
}

func (c *countingIngestor) ApplySequencedBoundary(
	context.Context,
	boyan.SequencedBoundary,
) (boyan.IngestOutcome, error) {
	c.boundaries.Add(1)

	return boyan.IngestOutcome{}, c.err
}

type invalidRequestCase struct {
	name  string
	path  string
	value any
	code  boyan.ErrorCode
}

func commonInvalidRequests() []invalidRequestCase {
	return []invalidRequestCase{
		{"envelope_schema", "schema_version", sensitiveMarker, boyan.CodeInvalidDerived},
		{"producer_missing", "producer_id", "", boyan.CodeInvalidDerived},
		{"producer_noncanonical", "producer_id", " producer ", boyan.CodeInvalidDerived},
		{"sequence_zero", "scope_sequence", 0, boyan.CodeInvalidDerived},
		{"payload_schema", "payload.schema_version", sensitiveMarker, boyan.CodePermanent},
		{"export_identity", "payload.export_id", sensitiveMarker, boyan.CodePermanent},
		{"scope_key", "payload.scope.key", "", boyan.CodeInvalidScope},
		{"scope_locator", "payload.scope.key", "unqualified", boyan.CodeInvalidScope},
		{"scope_kind", "payload.scope.kind", sensitiveMarker, boyan.CodeUnsupportedScope},
		{"conversation", "payload.conversation.conversation_id", "", boyan.CodeInvalidConversation},
		{"agent_conversation", "payload.conversation.agent_conversation_id", "", boyan.CodeInvalidConversation},
	}
}

func TestHandlerValidationOriginReturnsBadRequest(t *testing.T) {
	t.Parallel()

	for _, route := range []string{"turns", "boundaries"} {
		t.Run(route, func(t *testing.T) {
			cases := commonInvalidRequests()
			if route == "turns" {
				cases = append(cases,
					invalidRequestCase{"source_identity", "payload.source_id", sensitiveMarker, boyan.CodePermanent},
					invalidRequestCase{"completion_missing", "payload.completed_at", nil, boyan.CodePermanent},
					invalidRequestCase{"messages_missing", "payload.messages", nil, boyan.CodePermanent},
					invalidRequestCase{"message_identity", "payload.messages.0.message_id", sensitiveMarker, boyan.CodePermanent},
					invalidRequestCase{"text_missing", "payload.messages.0.text", " ", boyan.CodePermanent},
					invalidRequestCase{"terminal_status", "payload.terminal_status", sensitiveMarker, boyan.CodePermanent},
				)
			} else {
				cases = append(cases,
					invalidRequestCase{"reason", "payload.reason", sensitiveMarker, boyan.CodePermanent},
					invalidRequestCase{"occurrence_missing", "payload.occurred_at", nil, boyan.CodePermanent},
				)
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					var body map[string]any
					requireNoError(t, json.Unmarshal([]byte(validRequestJSON(t, route)), &body), "decode fixture: %v")
					setRequestField(body, tc.path, tc.value)

					counter := &countingIngestor{}
					rec := serveIngestRequest(t, route, mustJSON(t, body), counter)
					assertHTTPError(t, rec, http.StatusBadRequest, tc.code, boyan.ErrorClassPermanent, "ingest request is invalid")

					if counter.turns.Load() != 0 || counter.boundaries.Load() != 0 {
						t.Fatal("invalid request reached ingestion")
					}
				})
			}
		})
	}
}

func setRequestField(body map[string]any, path string, value any) {
	parts := strings.Split(path, ".")

	var current any = body

	for _, part := range parts[:len(parts)-1] {
		if part == "0" {
			current = current.([]any)[0]
		} else {
			current = current.(map[string]any)[part]
		}
	}

	current.(map[string]any)[parts[len(parts)-1]] = value
}

func validRequestJSON(t *testing.T, route string) string {
	t.Helper()

	if route == "turns" {
		turn := mustTurn(t, "validation-turn")
		turn.Messages[0].Text = sensitiveMarker

		return mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
			SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
			ProducerID:    "producer-1", ScopeSequence: 1, Payload: turn,
		})
	}

	return mustJSON(t, boyan.IngestEnvelope[boyan.Boundary]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1", ScopeSequence: 1, Payload: mustBoundary(t, "validation-boundary"),
	})
}

func serveIngestRequest(t *testing.T, route, body string, counter *countingIngestor) *httptest.ResponseRecorder {
	t.Helper()

	handler, err := httpingest.NewHandler(httpingest.Config{Turns: counter, Boundaries: counter})
	requireNoError(t, err, "NewHandler() error = %v")

	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/"+route, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	return rec
}

func assertHTTPError(
	t *testing.T, rec *httptest.ResponseRecorder, status int,
	code boyan.ErrorCode, class boyan.ErrorClass, message string,
) {
	t.Helper()

	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}

	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", rec.Header().Get("Content-Type"))
	}

	var body boyan.IngestErrorResponse
	requireNoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "decode error response: %v")

	if body.Code != code || body.Class != class || body.Message != message || body.RetryAfterMS != nil {
		t.Errorf("error response = %+v, want code=%s class=%s message=%q without retry metadata", body, code, class, message)
	}

	if strings.Contains(rec.Body.String(), sensitiveMarker) {
		t.Errorf("error response disclosed sensitive marker: %s", rec.Body.String())
	}
}
