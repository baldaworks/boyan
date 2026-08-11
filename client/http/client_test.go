package clienthttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestClientIngestTurnSuccess(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/ingest/turns" || r.Method != http.MethodPost {
			t.Fatalf("request = %s %s, want POST /v1/ingest/turns", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		_ = json.NewEncoder(w).Encode(boyan.IngestReceipt{
			SchemaVersion:  boyan.IngestReceiptSchemaVersionV1,
			ExportID:       mustTurn(t, "client-turn").ExportID,
			Disposition:    boyan.IngestDispositionDuplicate,
			Projection:     boyan.ProjectionStatusLagging,
			OperationID:    "boyan-memory:operation:client",
			OperationIDs:   []string{"boyan-memory:operation:client"},
			ScopeVersion:   3,
			ScopeChangeSeq: 4,
		})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	receipt, err := client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 3,
		Payload:       mustTurn(t, "client-turn"),
	})
	if err != nil {
		t.Fatalf("IngestTurn() error = %v", err)
	}
	if receipt.Disposition != boyan.IngestDispositionDuplicate || receipt.Projection != boyan.ProjectionStatusLagging || receipt.ScopeVersion != 3 || receipt.ScopeChangeSeq != 4 {
		t.Fatalf("receipt = %+v, want duplicate lagging response", receipt)
	}
}

func TestClientClassifiesErrorResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    int
		body      boyan.IngestErrorResponse
		wantCode  boyan.ErrorCode
		wantClass boyan.ErrorClass
	}{
		{name: "conflict", status: http.StatusConflict, body: boyan.IngestErrorResponse{Code: boyan.CodeConflict, Class: boyan.ErrorClassRetryable, Message: "conflict"}, wantCode: boyan.CodeConflict, wantClass: boyan.ErrorClassRetryable},
		{name: "bad request", status: http.StatusBadRequest, body: boyan.IngestErrorResponse{Code: boyan.CodeInvalidDerived, Class: boyan.ErrorClassPermanent, Message: "invalid"}, wantCode: boyan.CodeInvalidDerived, wantClass: boyan.ErrorClassPermanent},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: boyan.IngestErrorResponse{Code: boyan.CodeUnavailable, Class: boyan.ErrorClassRetryable, Message: "busy"}, wantCode: boyan.CodeUnavailable, wantClass: boyan.ErrorClassRetryable},
		{name: "timeout", status: http.StatusGatewayTimeout, body: boyan.IngestErrorResponse{Code: boyan.CodeTimeout, Class: boyan.ErrorClassRetryable, Message: "timeout"}, wantCode: boyan.CodeTimeout, wantClass: boyan.ErrorClassRetryable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_ = json.NewEncoder(w).Encode(tc.body)
			}))
			defer server.Close()
			client, err := New(Config{BaseURL: server.URL})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			_, err = client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
				SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
				ProducerID:    "producer-1",
				ScopeSequence: 1,
				Payload:       mustTurn(t, "client-error-turn"),
			})
			gotCode, gotClass, ok := boyan.ClassifyError(err)
			if !ok || gotCode != tc.wantCode || gotClass != tc.wantClass {
				t.Fatalf("error = (%q,%q,%v), want (%q,%q,true)", gotCode, gotClass, ok, tc.wantCode, tc.wantClass)
			}
		})
	}
}

func TestClientHealthProbes(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/livez":
			w.WriteHeader(http.StatusOK)
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(boyan.IngestErrorResponse{
				Code:    boyan.CodeUnavailable,
				Class:   boyan.ErrorClassRetryable,
				Message: "not ready",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := client.Live(context.Background()); err != nil {
		t.Fatalf("Live() error = %v", err)
	}
	err = client.Ready(context.Background())
	gotCode, gotClass, ok := boyan.ClassifyError(err)
	if !ok || gotCode != boyan.CodeUnavailable || gotClass != boyan.ErrorClassRetryable {
		t.Fatalf("Ready() error = (%q,%q,%v), want (%q,%q,true)", gotCode, gotClass, ok, boyan.CodeUnavailable, boyan.ErrorClassRetryable)
	}
}

func mustTurn(t *testing.T, turnID string) boyan.Turn {
	t.Helper()
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:client-turn", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "client-conversation", AgentConversationID: "client-agent"},
		turnID,
		time.Date(2026, time.August, 9, 13, 5, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	return turn
}
