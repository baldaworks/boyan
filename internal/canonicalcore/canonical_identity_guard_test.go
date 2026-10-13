package canonicalcore_test

import (
	"context"
	"errors"
	"testing"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	ordered "github.com/baldaworks/boyan/internal/orderedingest"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func offlineIdentityProcessor(t *testing.T, store *b.CanonicalStore) (*core.CanonicalTurnProcessor, *int) {
	t.Helper()

	calls := 0
	processor, err := core.NewCanonicalTurnProcessor(store, canonicalExtractorFunc(
		func(context.Context, m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
			calls++

			return nil, m.RetryableError(m.CodeModelFailure, "provider unavailable", nil)
		}), m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")

	return processor, &calls
}

func requirePermanentIdentityConflict(t *testing.T, err error) {
	t.Helper()

	var typed *m.Error
	if !errors.As(err, &typed) || typed.Code != m.CodeConflict || m.IsRetryable(err) {
		t.Fatalf("want permanent identity conflict, got %v", err)
	}
}

func changedIdentityTurn(turn m.Turn) m.Turn {
	turn.Messages = append([]m.Message(nil), turn.Messages...)
	turn.Messages[0].Text = "changed authoritative input"

	return turn
}

func TestCanonicalTurnIdentityRejectsChangeAndReplaysBeforeModel(t *testing.T) {
	t.Parallel()
	_, _, original, committed, store := nativeFlow(t)
	processor, calls := offlineIdentityProcessor(t, store)
	ctx := context.Background()
	before, err := store.LoadScopeState(ctx, original.Scope)
	requireNoError(t, err, "state before: %v")
	_, err = processor.ProcessTurn(ctx, changedIdentityTurn(original), testProcessorDerivation())
	requirePermanentIdentityConflict(t, err)
	replay, err := processor.ProcessTurn(ctx, original, testProcessorDerivation())
	requireNoError(t, err, "committed replay: %v")

	if *calls != 0 || replay.ScopeVersion != committed.ScopeVersion || replay.ChangeSeq != committed.ChangeSeq {
		t.Fatalf("model calls=%d, replay=%+v", *calls, replay)
	}

	after, err := store.LoadScopeState(ctx, original.Scope)
	requireNoError(t, err, "state after: %v")

	if after != before {
		t.Fatal("identity guard or replay changed canonical state")
	}
}

func TestSequencedTurnIdentityRejectsChangedReceiptAndProducerReplay(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"same-sequence", "next-sequence", "other-producer"} {
		t.Run(name, func(t *testing.T) { testSequencedIdentityGuard(t, name) })
	}
}

func testSequencedIdentityGuard(t *testing.T, name string) {
	t.Helper()
	_, _, original, committed, store := nativeFlow(t)
	seedIdentityReceipt(t, store, original, committed)
	processor, calls := offlineIdentityProcessor(t, store)
	runtime, err := ordered.NewSequencedIngestRuntime(store, store, processor,
		testProcessorDerivation(), nil, nil, nil, ordered.SequencedIngestConfig{})
	requireNoError(t, err, "runtime: %v")

	ctx := context.Background()

	requireNoError(t, runtime.Start(ctx), "start: %v")
	defer runtime.Close(ctx)

	request := m.SequencedTurn{ProducerID: "identity-producer", ScopeSequence: 1,
		Fingerprint: "same-delivery-fingerprint", Turn: changedIdentityTurn(original)}
	if name == "next-sequence" {
		request.ScopeSequence = 2
	}

	if name == "other-producer" {
		request.ProducerID = "other-producer"
	}

	_, err = runtime.IngestSequencedTurn(ctx, request)
	requirePermanentIdentityConflict(t, err)

	if *calls != 0 {
		t.Fatalf("identity mismatch called model %d times", *calls)
	}

	state, err := store.LoadScopeState(ctx, original.Scope)
	requireNoError(t, err, "canonical state: %v")

	if state.ChangeSeq != committed.ChangeSeq {
		t.Fatal("mismatch changed canonical sequence")
	}

	cursor, found, err := store.LoadLatestIngestSequence(ctx, request.ProducerID, original.Scope)
	requireNoError(t, err, "sequence cursor: %v")
	requireUnchangedIdentityCursor(t, name, cursor, found)
}

func seedIdentityReceipt(t *testing.T, store *b.CanonicalStore, turn m.Turn, committed m.CanonicalMutationOutcome) {
	t.Helper()

	operationID, err := m.CanonicalSemanticOperationID(turn.ExportID, testProcessorDerivation())
	requireNoError(t, err, "operation ID: %v")

	outcome := m.IngestOutcome{SchemaVersion: m.IngestOutcomeSchemaVersionV1,
		Kind: m.IngestKindTurn, Disposition: m.IngestDispositionAccepted, Projection: m.ProjectionStatusCurrent,
		ProducerID: "identity-producer", Scope: turn.Scope, ScopeSequence: 1,
		Fingerprint: "same-delivery-fingerprint", ExportID: turn.ExportID, OperationIDs: []string{operationID},
		ScopeVersion: committed.ScopeVersion, ScopeChangeSeq: committed.ChangeSeq}
	requireNoError(t, store.SaveIngestSequence(context.Background(), m.IngestSequenceRecord{
		SchemaVersion: m.IngestSequenceSchemaVersionV1, ProducerID: outcome.ProducerID, Scope: turn.Scope,
		Sequence: 1, Fingerprint: outcome.Fingerprint, Outcome: outcome}), "save existing receipt: %v")
}

func TestVerifiedTurnAllowsNewDerivationWithoutChangingSource(t *testing.T) {
	t.Parallel()
	_, _, original, committed, store := nativeFlow(t)
	calls := 0
	processor, err := core.NewCanonicalTurnProcessor(store, canonicalExtractorFunc(
		func(context.Context, m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
			calls++

			return nil, nil
		}), m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")

	derivation := testProcessorDerivation()
	derivation.Pipeline = "different-pipeline"
	outcome, err := processor.ProcessTurn(context.Background(), original, derivation)
	requireNoError(t, err, "verified new derivation: %v")

	if calls != 1 || outcome.ChangeSeq != committed.ChangeSeq+1 {
		t.Fatalf("calls=%d outcome=%+v", calls, outcome)
	}
}

func requireUnchangedIdentityCursor(t *testing.T, name string, cursor m.IngestSequenceCursor, found bool) {
	t.Helper()

	if (name == "other-producer" && found) || (name != "other-producer" && (!found || cursor.Latest != 1)) {
		t.Fatalf("mismatch advanced receipt cursor: found=%t cursor=%+v", found, cursor)
	}
}
