package canonicalcore_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/forgetops"
	"github.com/baldaworks/boyan/internal/httpingest"
	ordered "github.com/baldaworks/boyan/internal/orderedingest"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type turnReceiptFixture struct {
	factory      coreFactory
	path         string
	store        coreOwner
	runtime      *ordered.SequencedIngestRuntime
	turn         m.Turn
	calls        int
	processCalls int
}

type receiptCountingProcessor struct {
	*core.CanonicalTurnProcessor

	calls *int
}

func (p receiptCountingProcessor) ProcessTurn(ctx context.Context, turn m.Turn,
	derivation m.DerivationRef,
) (m.CanonicalMutationOutcome, error) {
	*p.calls++

	return p.CanonicalTurnProcessor.ProcessTurn(ctx, turn, derivation)
}

func newTurnReceiptFixture(t *testing.T, factory coreFactory) *turnReceiptFixture {
	t.Helper()
	f := &turnReceiptFixture{factory: factory, path: t.TempDir()}
	f.open(t)

	turn, err := m.NewTurn(m.Scope{Key: "receipt:old", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"old-turn", time.Now().UTC(), "receipt secret sentinel", "OK")
	requireNoError(t, err, "turn: %v")

	f.turn = turn

	t.Cleanup(func() { f.close(t) })

	return f
}

func (f *turnReceiptFixture) open(t *testing.T) {
	t.Helper()

	var err error

	if f.factory != nil {
		f.store, _ = f.factory(t, f.path)
	} else {
		f.store, err = b.Open(f.path)
		requireNoError(t, err, "open: %v")
	}

	processor, err := core.NewCanonicalTurnProcessor(f.store, canonicalExtractorFunc(
		func(ctx context.Context, request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
			f.calls++

			return extractTestStateCandidate(ctx, request)
		}), m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	f.runtime, err = ordered.NewSequencedIngestRuntime(f.store, f.store,
		receiptCountingProcessor{CanonicalTurnProcessor: processor, calls: &f.processCalls},
		testProcessorDerivation(), nil, nil, nil, ordered.SequencedIngestConfig{})
	requireNoError(t, err, "runtime: %v")
	requireNoError(t, f.runtime.Start(context.Background()), "start: %v")
}

func (f *turnReceiptFixture) close(t *testing.T) {
	t.Helper()
	requireNoError(t, f.runtime.Close(context.Background()), "close runtime: %v")
	requireNoError(t, f.store.Close(), "close store: %v")
}

func (f *turnReceiptFixture) post(t *testing.T, turn m.Turn) *httptest.ResponseRecorder {
	t.Helper()

	handler, err := httpingest.NewHandler(httpingest.Config{Turns: f.runtime, Boundaries: f.runtime})
	requireNoError(t, err, "handler: %v")
	body, err := json.Marshal(m.IngestEnvelope[m.Turn]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: "old-producer", ScopeSequence: 1, Payload: turn})
	requireNoError(t, err, "marshal: %v")

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/ingest/turns", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)

	return response
}

func (f *turnReceiptFixture) record(t *testing.T) m.IngestSequenceRecord {
	t.Helper()

	record, found, err := f.store.LoadIngestSequence(context.Background(), "old-producer", f.turn.Scope, 1)
	requireNoError(t, err, "receipt: %v")

	if !found {
		t.Fatal("missing receipt")
	}

	return record
}

func TestTurnHTTPReceiptCompatibilityUsesAuthoritativeProof(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, kind := range []string{"exact", "normalized", "restart", "forgotten"} {
			t.Run(kind, func(t *testing.T) { testTurnReceiptCompatibility(t, kind, factory) })
		}
	})
}

func testTurnReceiptCompatibility(t *testing.T, kind string, factory coreFactory) {
	t.Helper()
	f := newTurnReceiptFixture(t, factory)

	raw := legacyReceiptWireTurn(f.turn)
	if response := f.post(t, raw); response.Code != http.StatusOK {
		t.Fatalf("first: %d %s", response.Code, response.Body)
	}

	seedLegacyRawTurnReceipt(t, f, raw)
	original := f.record(t)

	operation := receiptCanonicalOperation(t, f, original.Outcome.OperationIDs[0])
	if kind == "restart" {
		f.close(t)
		f.open(t)
	}

	if kind == "forgotten" {
		forgetTurnReceiptFixture(t, f)
	}

	before, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "before: %v")

	retry := f.turn
	if kind == "exact" {
		retry = raw
	}

	response := f.post(t, retry)
	if response.Code != http.StatusOK {
		t.Fatalf("replay: %d %s", response.Code, response.Body)
	}

	assertTurnReceiptReplayUnchanged(t, f, original, before, response)

	if !reflect.DeepEqual(operation, receiptCanonicalOperation(t, f, original.Outcome.OperationIDs[0])) {
		t.Fatal("receipt replay changed canonical operation or revision IDs")
	}
}

func legacyReceiptWireTurn(turn m.Turn) m.Turn {
	turn.SourceID = ""
	turn.TerminalStatus = ""

	turn.Messages = append([]m.Message(nil), turn.Messages...)
	for i := range turn.Messages {
		turn.Messages[i].MessageID = ""
	}

	turn.CompletedAt = turn.CompletedAt.In(time.FixedZone("old-wire", 6*60*60))

	return turn
}

func assertTurnReceiptReplayUnchanged(t *testing.T, f *turnReceiptFixture,
	original m.IngestSequenceRecord, before m.ScopeState, response *httptest.ResponseRecorder,
) {
	t.Helper()

	var receipt m.IngestReceipt
	requireNoError(t, json.Unmarshal(response.Body.Bytes(), &receipt), "response: %v")

	expected := original.Outcome
	expected.Disposition = m.IngestDispositionDuplicate
	want, err := m.IngestReceiptFromOutcome(expected)
	requireNoError(t, err, "expected receipt: %v")
	after, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "after: %v")

	if !reflect.DeepEqual(receipt, want) || !reflect.DeepEqual(f.record(t), original) ||
		before != after || f.calls != 1 || f.processCalls != 1 {
		t.Fatalf("replay changed receipt/state/model: receipt=%+v calls=%d", receipt, f.calls)
	}
}

func forgetTurnReceiptFixture(t *testing.T, f *turnReceiptFixture) {
	t.Helper()

	adapter, err := forgetops.NewCanonicalForgetAdapter(f.store)
	requireNoError(t, err, "forget adapter: %v")
	_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
		SchemaVersion: m.DerivedSchemaVersionV1, Scope: f.turn.Scope, RequestID: "receipt-forget",
		ForgottenAt: time.Now().UTC()})
	requireNoError(t, err, "forget: %v")
}

func receiptCanonicalOperation(t *testing.T, f *turnReceiptFixture, id string) m.CanonicalOperationRecord {
	t.Helper()

	operation, found, err := f.store.LoadCanonicalOperation(context.Background(), f.turn.Scope, id)
	requireNoError(t, err, "operation: %v")

	if !found {
		t.Fatal("missing canonical operation")
	}

	return operation
}

// Preserve the exact pre-upgrade raw JSON hash independently of the current HTTP handler.
func seedLegacyRawTurnReceipt(t *testing.T, f *turnReceiptFixture, raw m.Turn) {
	t.Helper()

	encoded, err := json.Marshal(raw)
	requireNoError(t, err, "legacy raw turn: %v")

	sum := sha256.Sum256(append([]byte("turn\x00"), encoded...))
	legacy := "boyan-ingest-fingerprint:" + hex.EncodeToString(sum[:])
	record := f.record(t)
	record.Fingerprint = legacy
	record.Outcome.Fingerprint = legacy
	writeTurnReceiptFixture(t, f, record)
	current, err := m.TurnIngestFingerprint(m.IngestEnvelope[m.Turn]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: record.ProducerID, ScopeSequence: record.Sequence, Payload: raw})
	requireNoError(t, err, "current transport fingerprint: %v")

	if legacy == current || f.record(t).Fingerprint != legacy {
		t.Fatal("old receipt fixture did not retain a distinct raw hash")
	}
}
