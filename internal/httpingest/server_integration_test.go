//go:build integration

package httpingest_test

import (
	"context"
	"net/http/httptest"
	"testing"

	clienthttp "github.com/baldaworks/boyan/client/http"
	httpingest "github.com/baldaworks/boyan/internal/httpingest"
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
