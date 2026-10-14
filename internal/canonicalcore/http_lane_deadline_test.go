package canonicalcore_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/httpingest"
	ordered "github.com/baldaworks/boyan/internal/orderedingest"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type deadlineCaptureIngestor struct {
	runtime  *ordered.SequencedIngestRuntime
	contexts chan context.Context
}

func (a deadlineCaptureIngestor) IngestSequencedTurn(ctx context.Context,
	turn m.SequencedTurn,
) (m.IngestOutcome, error) {
	a.contexts <- ctx

	return a.runtime.IngestSequencedTurn(ctx, turn)
}

func TestHTTPQueuedIngestDeadlineReturnsBeforeLivePredecessor(t *testing.T) {
	runtime, store, second, firstDone := livePredecessorRuntime(t)
	captured := make(chan context.Context, 1)
	handler, err := httpingest.NewHandler(httpingest.Config{
		Turns:      deadlineCaptureIngestor{runtime: runtime, contexts: captured},
		Boundaries: runtime, RequestTimeout: 100 * time.Millisecond,
	})
	requireNoError(t, err, "NewHandler() = %v")
	body, err := json.Marshal(m.IngestEnvelope[m.Turn]{
		SchemaVersion: m.IngestHTTPSchemaVersionV1, ProducerID: "deadline", ScopeSequence: 2, Payload: second,
	})
	requireNoError(t, err, "Marshal() = %v")

	request := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	response := httptest.NewRecorder()
	httpDone := make(chan struct{})

	go func() { handler.ServeHTTP(response, request); close(httpDone) }()

	processing := awaitIngestContext(t, captured)
	awaitDeadlineSignal(t, processing.Done())
	awaitDeadlineSignal(t, httpDone)

	select {
	case <-firstDone:
		t.Fatal("predecessor returned before release")
	default:
	}

	if response.Code != http.StatusGatewayTimeout {
		t.Fatalf("response = %d %s", response.Code, response.Body)
	}

	if _, found, err := store.LoadIngestSequence(context.Background(), "deadline", second.Scope, 2); err != nil || found {
		t.Fatalf("canceled request receipt: found=%v err=%v", found, err)
	}
}

func livePredecessorRuntime(t *testing.T) (*ordered.SequencedIngestRuntime, *b.CanonicalStore, m.Turn, <-chan error) {
	t.Helper()
	store, err := b.Open(t.TempDir())
	requireNoError(t, err, "Open() = %v")
	t.Cleanup(func() { _ = store.Close() })

	started, release := make(chan struct{}), make(chan struct{})
	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest,
	) ([]m.SemanticCandidate, error) {
		if request.Turn.SourceTurnID == "first" {
			close(started)
			<-release
		}

		return extractTestStateCandidate(ctx, request)
	})
	processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "NewCanonicalTurnProcessor() = %v")
	runtime, err := ordered.NewSequencedIngestRuntime(store, store, processor, testProcessorDerivation(),
		nil, nil, nil, ordered.SequencedIngestConfig{})
	requireNoError(t, err, "NewSequencedIngestRuntime() = %v")
	requireNoError(t, runtime.Start(context.Background()), "Start() = %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	scope := m.Scope{Key: "test:http-deadline", Kind: m.ScopeKindPersonal}
	conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
	first, err := m.NewTurn(scope, conversation, "first", time.Now().UTC(), "memory first", "OK")
	requireNoError(t, err, "NewTurn(first) = %v")
	second, err := m.NewTurn(scope, conversation, "second", first.CompletedAt.Add(time.Minute), "memory second", "OK")
	requireNoError(t, err, "NewTurn(second) = %v")

	firstDone := make(chan error, 1)

	go func() {
		_, err := runtime.IngestSequencedTurn(context.Background(), m.SequencedTurn{
			ProducerID: "deadline", ScopeSequence: 1, Fingerprint: "first", Turn: first,
		})
		firstDone <- err
	}()

	t.Cleanup(func() {
		close(release)

		select {
		case err := <-firstDone:
			requireNoError(t, err, "predecessor = %v")
		case <-time.After(time.Second):
			t.Error("predecessor did not drain")
		}
	})
	awaitDeadlineSignal(t, started)

	return runtime, store, second, firstDone
}

func awaitDeadlineSignal(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("HTTP ingest did not progress while predecessor remained blocked")
	}
}

func awaitIngestContext(t *testing.T, contexts <-chan context.Context) context.Context {
	t.Helper()

	select {
	case ctx := <-contexts:
		return ctx
	case <-time.After(time.Second):
		t.Fatal("HTTP ingest context was not captured")

		return nil
	}
}
