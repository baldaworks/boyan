package httpingest_test

import httpingest "github.com/baldaworks/boyan/internal/httpingest"

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	clienthttp "github.com/baldaworks/boyan/client/http"
	orderedingest "github.com/baldaworks/boyan/internal/orderedingest"
	boyan "github.com/baldaworks/boyan/pkg/memory"
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
		OperationIDs:   []string{"boyan-memory:operation:turn"},
		ScopeVersion:   1,
		ScopeChangeSeq: 1,
	}}

	handler, err := httpingest.NewHandler(httpingest.Config{Turns: turns, Boundaries: &fakeBoundaryIngestor{}})
	requireNoError(t, err, "NewHandler() error = %v")

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	requireNoError(t, err, "clienthttp.New() error = %v")

	receipt, err := client.IngestTurn(context.Background(), boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "http-turn"),
	})
	requireNoError(t, err, "Client.IngestTurn() error = %v")

	if receipt.ExportID != mustTurn(t, "http-turn").ExportID ||
		receipt.Disposition != boyan.IngestDispositionAccepted ||
		receipt.Projection != boyan.ProjectionStatusCurrent ||
		receipt.OperationID == "" {
		t.Fatalf("receipt = %+v, want accepted typed response", receipt)
	}

	if turns.request.ProducerID != "producer-1" || turns.request.ScopeSequence != 1 ||
		turns.request.Turn.ExportID != mustTurn(t, "http-turn").ExportID ||
		turns.request.Fingerprint == "" {
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

	handler, err := httpingest.NewHandler(httpingest.Config{Turns: &fakeTurnIngestor{}, Boundaries: boundaries})
	requireNoError(t, err, "NewHandler() error = %v")

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	requireNoError(t, err, "clienthttp.New() error = %v")

	receipt, err := client.IngestBoundary(context.Background(), boyan.IngestEnvelope[boyan.Boundary]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       boundary,
	})
	requireNoError(t, err, "Client.IngestBoundary() error = %v")

	if receipt.ExportID != boundary.ExportID || len(receipt.OperationIDs) != 2 ||
		receipt.ScopeVersion != 2 ||
		receipt.ScopeChangeSeq != 3 {
		t.Fatalf("receipt = %+v, want boundary response", receipt)
	}

	if boundaries.request.Boundary.ExportID != boundary.ExportID ||
		boundaries.request.Fingerprint == "" {
		t.Fatalf(
			"boundary request = %+v, want sequenced boundary with fingerprint",
			boundaries.request,
		)
	}
}

func TestHandlerRejectsUnknownFields(t *testing.T) {
	t.Parallel()

	handler, err := httpingest.NewHandler(
		httpingest.Config{
			Turns:      &fakeTurnIngestor{},
			Boundaries: &fakeBoundaryIngestor{},
		},
	)
	requireNoError(t, err, "NewHandler() error = %v")

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/ingest/turns",
		strings.NewReader(
			("{\"schema_version\":\"boyan-ingest/v1\",\"producer_id\":\"producer-" +
				"1\",\"scope_sequence\":1,\"payload\":{\"schema_version\":\"boyan/v1\"" +
				",\"export_id\":\"bad\",\"unknown\":\"field\"}}"),
		),
	)
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

	tests := handlerErrorCases()

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler, err := httpingest.NewHandler(tc.cfg)
			requireNoError(t, err, "NewHandler() error = %v")

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

			if strings.Contains(rec.Body.String(), "secret raw text") ||
				strings.Contains(rec.Body.String(), "raw text") {
				t.Fatalf("response leaked raw text: %s", rec.Body.String())
			}
		})
	}
}

func TestHandlerRejectsOversizedBody(t *testing.T) {
	t.Parallel()

	handler, err := httpingest.NewHandler(
		httpingest.Config{Turns: &fakeTurnIngestor{}, Boundaries: &fakeBoundaryIngestor{}, MaxBodyBytes: 16},
	)
	requireNoError(t, err, "NewHandler() error = %v")

	body := mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "oversized-turn"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader(body))
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

	handler, err := httpingest.NewHandler(httpingest.Config{
		Turns: &fakeTurnIngestor{process: func(
			ctx context.Context,
			request boyan.SequencedTurn,
		) (
			boyan.IngestOutcome,
			error,
		) {
			<-ctx.Done()

			return boyan.IngestOutcome{}, boyan.RetryableError(boyan.CodeTimeout, "delayed request", ctx.Err())
		}},
		Boundaries:     &fakeBoundaryIngestor{},
		RequestTimeout: 10 * time.Millisecond,
	})
	requireNoError(t, err, "NewHandler() error = %v")

	body := mustJSON(t, boyan.IngestEnvelope[boyan.Turn]{
		SchemaVersion: boyan.IngestHTTPSchemaVersionV1,
		ProducerID:    "producer-1",
		ScopeSequence: 1,
		Payload:       mustTurn(t, "timeout-turn"),
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", strings.NewReader(body))
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
		handler, err := httpingest.NewHandler(httpingest.Config{Turns: backend, Boundaries: &fakeBoundaryIngestor{}})
		requireNoError(t, err, "NewHandler() error = %v")

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
	requireNoError(t, err, "clienthttp.New() error = %v")

	first, err := client.IngestTurn(context.Background(), envelope)

	server.Close()

	requireNoError(t, err, "first IngestTurn() error = %v")

	if first.Disposition != boyan.IngestDispositionAccepted {
		t.Fatalf("first receipt = %+v, want accepted", first)
	}

	server = makeServer()
	defer server.Close()

	client, err = clienthttp.New(clienthttp.Config{BaseURL: server.URL})
	requireNoError(t, err, "clienthttp.New(restart) error = %v")

	second, err := client.IngestTurn(context.Background(), envelope)
	requireNoError(t, err, "second IngestTurn() error = %v")

	if second.Disposition != boyan.IngestDispositionDuplicate ||
		second.ExportID != first.ExportID {
		t.Fatalf("second receipt = %+v, want duplicate replay of first", second)
	}
}

func TestValidateLoopbackAddressRejectsUnsafeHosts(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{"127.0.0.1:0", "[::1]:8080", "localhost:9090"} {
		if err := httpingest.ValidateLoopbackAddress(addr); err != nil {
			t.Fatalf("ValidateLoopbackAddress(%q) error = %v", addr, err)
		}
	}

	for _, addr := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "example.com:8080"} {
		if err := httpingest.ValidateLoopbackAddress(addr); err == nil {
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

func (f *fakeTurnIngestor) IngestSequencedTurn(
	ctx context.Context,
	request boyan.SequencedTurn,
) (
	boyan.IngestOutcome,
	error,
) {
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

func (f *fakeBoundaryIngestor) ApplySequencedBoundary(
	_ context.Context,
	request boyan.SequencedBoundary,
) (
	boyan.IngestOutcome,
	error,
) {
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

func (p *persistentTurnReplayIngestor) IngestSequencedTurn(
	_ context.Context,
	request boyan.SequencedTurn,
) (
	boyan.IngestOutcome,
	error,
) {
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
			OperationIDs:   []string{"boyan-memory:operation:restart"},
			ScopeVersion:   1,
			ScopeChangeSeq: 1,
		}

		return p.outcome, nil
	}

	replayed := p.outcome
	replayed.Disposition = boyan.IngestDispositionDuplicate

	return replayed, nil
}

var _ orderedingest.TurnIngestor = (*fakeTurnIngestor)(nil)
var _ orderedingest.BoundaryIngestor = (*fakeBoundaryIngestor)(nil)

func mustTurn(t *testing.T, turnID string) boyan.Turn {
	t.Helper()

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-http", AgentConversationID: "agent-http"},
		turnID,
		time.Date(2026, time.August, 9, 13, 0, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	return turn
}

func mustBoundary(t *testing.T, transitionID string) boyan.Boundary {
	t.Helper()

	boundary, err := boyan.NewBoundary(
		boyan.Scope{Key: "telegram:http-turn", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "conversation-http", AgentConversationID: "agent-http"},
		transitionID,
		boyan.BoundaryReasonClose,
		time.Date(2026, time.August, 9, 13, 1, 0, 0, time.UTC),
	)
	requireNoError(t, err, "NewBoundary() error = %v")

	return boundary
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	requireNoError(t, err, "json.Marshal() error = %v")

	return string(encoded)
}

func assertSafeErrorBody(
	t *testing.T,
	rec *httptest.ResponseRecorder,
	wantCode boyan.ErrorCode,
	wantClass boyan.ErrorClass,
) {
	t.Helper()

	var body boyan.IngestErrorResponse
	requireNoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "json.Unmarshal(error body) error = %v")

	if body.Code != wantCode || body.Class != wantClass || body.Message == "" {
		t.Fatalf("error body = %+v, want code=%q class=%q", body, wantCode, wantClass)
	}
}

type handlerErrorCase struct {
	name       string
	targetPath string
	cfg        httpingest.Config
	wantStatus int
	wantCode   boyan.ErrorCode
	wantClass  boyan.ErrorClass
}

func handlerErrorCases() []handlerErrorCase {
	return []handlerErrorCase{
		{
			name:       "conflict",
			targetPath: "/v1/ingest/turns",
			cfg: httpingest.Config{
				Turns:      &fakeTurnIngestor{err: boyan.RetryableError(boyan.CodeConflict, "ingest sequence gap", nil)},
				Boundaries: &fakeBoundaryIngestor{},
			},
			wantStatus: http.StatusConflict,
			wantCode:   boyan.CodeConflict,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "backpressure",
			targetPath: "/v1/ingest/turns",
			cfg: httpingest.Config{
				Turns: &fakeTurnIngestor{
					err: boyan.RetryableError(boyan.CodeLimitExceeded, "queue full with secret raw text", nil),
				},
				Boundaries: &fakeBoundaryIngestor{},
			},
			wantStatus: http.StatusTooManyRequests,
			wantCode:   boyan.CodeLimitExceeded,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "timeout",
			targetPath: "/v1/ingest/turns",
			cfg: httpingest.Config{
				Turns:      &fakeTurnIngestor{err: boyan.RetryableError(boyan.CodeTimeout, "slow model with raw text", nil)},
				Boundaries: &fakeBoundaryIngestor{},
			},
			wantStatus: http.StatusGatewayTimeout,
			wantCode:   boyan.CodeTimeout,
			wantClass:  boyan.ErrorClassRetryable,
		},
		{
			name:       "readyz",
			targetPath: "/readyz",
			cfg: httpingest.Config{
				Turns:      &fakeTurnIngestor{},
				Boundaries: &fakeBoundaryIngestor{},
				Ready: readyCheckerFunc(
					func(context.Context) error { return boyan.RetryableError(boyan.CodeUnavailable, "not ready", nil) },
				),
			},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   boyan.CodeUnavailable,
			wantClass:  boyan.ErrorClassRetryable,
		},
	}
}
