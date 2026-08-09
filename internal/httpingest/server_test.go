package httpingest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clienthttp "github.com/baldaworks/boyan/client/http"
	portableapp "github.com/baldaworks/boyan/app"
	"github.com/baldaworks/boyan"
)

func TestHandlerTurnSuccessAndTypedClientRoundTrip(t *testing.T) {
	t.Parallel()

	turns := &fakeTurnIngestor{outcome: boyan.IngestOutcome{
		SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
		Kind:           boyan.IngestKindTurn,
		Disposition:    boyan.IngestDispositionAccepted,
		Projection:     boyan.ProjectionStatusCurrent,
		ProducerID:     "producer-1",
		Scope:          boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal},
		ScopeSequence:  1,
		Fingerprint:    "boyan-ingest-fingerprint:1",
		ExportID:       mustTurn(t, "http-turn").ExportID,
		OperationIDs:   []string{"session-memory:v2:operation:turn"},
		ScopeVersion:   1,
		ScopeChangeSeq: 1,
	}}
	handler, err := NewHandler(Config{Turns: turns, Boundaries: &fakeBoundaryIngestor{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("clienthttp.New() error = %v", err)
	}
	receipt, err := client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "http-turn"),
	})
	if err != nil {
		t.Fatalf("Client.IngestTurn() error = %v", err)
	}
	if receipt.ExportID != mustTurn(t, "http-turn").ExportID || receipt.Disposition != boyan.IngestDispositionAccepted || receipt.Projection != boyan.ProjectionStatusCurrent || receipt.OperationID == "" {
		t.Fatalf("receipt = %+v, want accepted typed response", receipt)
	}
	if turns.request.ProducerID != "producer-1" || turns.request.ScopeSequence != 1 || turns.request.Turn.ExportID != mustTurn(t, "http-turn").ExportID || turns.request.Fingerprint == "" {
		t.Fatalf("turn request = %+v, want sequenced request with derived fingerprint", turns.request)
	}
}

func TestHandlerBoundarySuccessAndTypedClientRoundTrip(t *testing.T) {
	t.Parallel()

	boundary := mustBoundary(t, "boundary-1")
	boundaries := &fakeBoundaryIngestor{outcome: boyan.IngestOutcome{
		SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
		Kind:           boyan.IngestKindBoundary,
		Disposition:    boyan.IngestDispositionAccepted,
		Projection:     boyan.ProjectionStatusCurrent,
		ProducerID:     "producer-1",
		Scope:          boundary.Scope,
		ScopeSequence:  1,
		Fingerprint:    "boyan-ingest-fingerprint:boundary",
		ExportID:       boundary.ExportID,
		OperationIDs:   []string{"scenario-op", "profile-op"},
		ScopeVersion:   2,
		ScopeChangeSeq: 3,
	}}
	handler, err := NewHandler(Config{Turns: &fakeTurnIngestor{}, Boundaries: boundaries})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("clienthttp.New() error = %v", err)
	}
	receipt, err := client.IngestBoundary(context.Background(), boyan.IngestEnvelope[boyan.Boundary]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       boundary,
	})
	if err != nil {
		t.Fatalf("Client.IngestBoundary() error = %v", err)
	}
	if receipt.ExportID != boundary.ExportID || len(receipt.OperationIDs) != 2 || receipt.ScopeVersion != 2 || receipt.ScopeChangeSeq != 3 {
		t.Fatalf("receipt = %+v, want boundary response", receipt)
	}
	if boundaries.request.Boundary.ExportID != boundary.ExportID || boundaries.request.Fingerprint == "" {
		t.Fatalf("boundary request = %+v, want sequenced boundary with fingerprint", boundaries.request)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(Config{Turns: &fakeTurnIngestor{}, Boundaries: &fakeBoundaryIngestor{}})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader(`{"schema_version":"boyan-ingest/v1","producer_id":"producer-1","scope_sequence":1,"payload":{"schema_version":"session-memory/v1","export_id":"bad","unknown":"field"}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertSafeErrorBody(t, rec, boyan.CodeInvalidDerived, boyan.ErrorClassPermanent)
}

func TestHandlerMapsConflictBackpressureTimeoutAndReadiness(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		targetPath string
		cfg        Config
		wantStatus int
		wantCode   boyan.ErrorCode
		wantClass  boyan.ErrorClass
	}{
		{
			name:       "conflict",
			targetPath: "/v1/ingest/turns",
			cfg:        Config{Turns: &fakeTurnIngestor{err: boyan.RetryableError(boyan.CodeConflict, "ingest sequence gap", nil)}, Boundaries: &fakeBoundaryIngestor{}},
			wantStatus: http.StatusConflict,
			wantCode:   boyan.CodeConflict,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "backpressure",
			targetPath: "/v1/ingest/turns",
			cfg:        Config{Turns: &fakeTurnIngestor{err: boyan.RetryableError(boyan.CodeLimitExceeded, "queue full with secret raw text", nil)}, Boundaries: &fakeBoundaryIngestor{}},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   boyan.CodeLimitExceeded,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "timeout",
			targetPath: "/v1/ingest/turns",
			cfg:        Config{Turns: &fakeTurnIngestor{err: boyan.RetryableError(boyan.CodeTimeout, "slow model with raw text", nil)}, Boundaries: &fakeBoundaryIngestor{}},
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   boyan.CodeTimeout,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "readyz",
			targetPath: "/readyz",
			cfg:        Config{Turns: &fakeTurnIngestor{}, Boundaries: &fakeBoundaryIngestor{}, Ready: readyCheckerFunc(func(context.Context) error { return boyan.RetryableError(boyan.CodeUnavailable, "not ready", nil) })},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   boyan.CodeUnavailable,
			wantClass:  boyan.ErrorClassRetryable,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := NewHandler(tc.cfg)
			if err != nil {
				t.Fatalf("NewHandler() error = %v", err)
			}
			var req *http.Request
			if tc.targetPath == "/readyz" {
				req = httptest.NewRequest(http.MethodGet, tc.targetPath, nil)
			} else {
				body := mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
					SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
					ProducerID:    "producer-1",
					ScopeSequence: 1,
					Payload:       mustTurn(t, "mapping-turn"),
				})
				req = httptest.NewRequest(http.MethodPost, tc.targetPath, strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			assertSafeErrorBody(t, rec, tc.wantCode, tc.wantClass)
			if strings.Contains(rec.Body.String(), "secret raw text") || strings.Contains(rec.Body.String(), "raw text") {
				t.Fatalf("response leaked raw text: %s", rec.Body.String())
			}
		})
	}
}

func TestHandlerRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(Config{Turns: &fakeTurnIngestor{}, Boundaries: &fakeBoundaryIngestor{}, MaxBodyBytes: 16})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader(mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "oversized-turn"),
	})))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertSafeErrorBody(t, rec, boyan.CodeLimitExceeded, boyan.ErrorClassPermanent)
}

func TestHandlerRequestTimeoutUsesContextDeadline(t *testing.T) {
	t.Parallel()

	handler, err := NewHandler(Config{
		Turns: &fakeTurnIngestor{process: func(ctx context.Context, request boyan.SequencedTurn) (boyan.IngestOutcome, error) {
			<-ctx.Done()
			return boyan.IngestOutcome{}, boyan.RetryableError(boyan.CodeTimeout, "delayed request", ctx.Err())
		}},
		Boundaries:     &fakeBoundaryIngestor{},
		RequestTimeout: 10 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader(mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "timeout-turn"),
	})))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rec.Code)
	}
	assertSafeErrorBody(t, rec, boyan.CodeTimeout, boyan.ErrorClassRetryable)
}

func TestHandlerReplaysDuplicateTurnAcrossHandlerRestart(t *testing.T) {
	t.Parallel()

	backend := &persistentTurnReplayIngestor{scope: boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal}}
	makeServer := func() *httptest.Server {
		handler, err := NewHandler(Config{Turns: backend, Boundaries: &fakeBoundaryIngestor{}})
		if err != nil {
			t.Fatalf("NewHandler() error = %v", err)
		}
		return httptest.NewServer(handler)
	}

	envelope := boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "restart-turn"),
	}

	server := makeServer()
	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("clienthttp.New() error = %v", err)
	}
	first, err := client.IngestTurn(context.Background(), envelope)
	server.Close()
	if err != nil {
		t.Fatalf("first IngestTurn() error = %v", err)
	}
	if first.Disposition != boyan.IngestDispositionAccepted {
		t.Fatalf("first receipt = %+v, want accepted", first)
	}

	server = makeServer()
	defer server.Close()
	client, err = clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("clienthttp.New(restart) error = %v", err)
	}
	second, err := client.IngestTurn(context.Background(), envelope)
	if err != nil {
		t.Fatalf("second IngestTurn() error = %v", err)
	}
	if second.Disposition != boyan.IngestDispositionDuplicate || second.ExportID != first.ExportID {
		t.Fatalf("second receipt = %+v, want duplicate replay of first", second)
	}
}

func TestValidateLoopbackAddressRejectsUnsafeHosts(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"127.0.0.1:0", "[::1]:8080", "localhost:9090"} {
		if err := ValidateLoopbackAddress(addr); err != nil {
			t.Fatalf("ValidateLoopbackAddress(%q) error = %v", addr, err)
		}
	}
	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "example.com:8080"} {
		if err := ValidateLoopbackAddress(addr); err == nil {
			t.Fatalf("ValidateLoopbackAddress(%q) error = nil, want rejection", addr)
		}
	}
}

type fakeTurnIngestor struct {
	request boyan.SequencedTurn
	outcome boyan.IngestOutcome
	err     error
	process func(context.Context, boyan.SequencedTurn) (boyan.IngestOutcome, error)
}

func (f *fakeTurnIngestor) IngestSequencedTurn(ctx context.Context, request boyan.SequencedTurn) (boyan.IngestOutcome, error) {
	f.request = request
	if f.process != nil {
		return f.process(ctx, request)
	}
	return f.outcome, f.err
}

type fakeBoundaryIngestor struct {
	request boyan.SequencedBoundary
	outcome boyan.IngestOutcome
	err     error
}

func (f *fakeBoundaryIngestor) ApplySequencedBoundary(_ context.Context, request boyan.SequencedBoundary) (boyan.IngestOutcome, error) {
	f.request = request
	return f.outcome, f.err
}

type readyCheckerFunc func(context.Context) error

func (f readyCheckerFunc) CheckReadiness(ctx context.Context) error { return f(ctx) }

type persistentTurnReplayIngestor struct {
	scope   boyan.Scope
	seen    bool
	outcome boyan.IngestOutcome
}

func (p *persistentTurnReplayIngestor) IngestSequencedTurn(_ context.Context, request boyan.SequencedTurn) (boyan.IngestOutcome, error) {
	if !p.seen {
		p.seen = true
		p.outcome = boyan.IngestOutcome{
			SchemaVersion:  boyan.IngestOutcomeSchemaVersionV1,
			Kind:           boyan.IngestKindTurn,
			Disposition:    boyan.IngestDispositionAccepted,
			Projection:     boyan.ProjectionStatusCurrent,
			ProducerID:     request.ProducerID,
			Scope:          request.Turn.Scope,
			ScopeSequence:  request.ScopeSequence,
			Fingerprint:    request.Fingerprint,
			ExportID:       request.Turn.ExportID,
			OperationIDs:   []string{"session-memory:v2:operation:restart"},
			ScopeVersion:   1,
			ScopeChangeSeq: 1,
		}
		return p.outcome, nil
	}
	replayed := p.outcome
	replayed.Disposition = boyan.IngestDispositionDuplicate
	return replayed, nil
}

var _ portableapp.SequencedTurnIngestor = (*fakeTurnIngestor)(nil)
var _ portableapp.SequencedBoundaryIngestor = (*fakeBoundaryIngestor)(nil)

func mustTurn(t *testing.T, turnID string) boyan.Turn {
	t.Helper()
	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-http", AgentSessionID: "agent-http"},
		turnID,
		time.Date(2026, time.August, 9, 13, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	return turn
}

func mustBoundary(t *testing.T, transitionID string) boyan.Boundary {
	t.Helper()
	boundary, err := boyan.NewBoundary(
		boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal},
		boyan.SessionRef{SessionID: "session-http", AgentSessionID: "agent-http"},
		transitionID,
		boyan.BoundaryReasonClose,
		time.Date(2026, time.August, 9, 13, 1, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}
	return boundary
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(encoded)
}

func assertSafeErrorBody(t *testing.T, rec *httptest.ResponseRecorder, wantCode boyan.ErrorCode, wantClass boyan.ErrorClass) {
	t.Helper()
	var body boyan.IngestErrorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal(error body) error = %v", err)
	}
	if body.Code != wantCode || body.Class != wantClass || body.Message == "" {
		t.Fatalf("error body = %+v, want code=%q class=%q", body, wantCode, wantClass)
	}
}
