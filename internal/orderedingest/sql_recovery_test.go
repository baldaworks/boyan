//go:build integration

package orderedingest_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/internal/orderedingest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestSQLOrderedTurnCommitBeforeReceiptRecovery(t *testing.T) {
	for _, reopen := range []bool{false, true} {
		name := "same-process-repeated-failures"
		if reopen {
			name = "close-reopen"
		}

		t.Run(name, func(t *testing.T) {
			storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
				testSQLTurnRecovery(t, fixture, reopen)
			})
		})
	}
}

func testSQLTurnRecovery(t *testing.T, fixture *storefixture.Fixture, reopen bool) {
	t.Helper()

	store := fixture.Store.(sqlIngestStore)
	counts := &sqlProcessingCounts{}
	runtime := newSQLIngestRuntime(t, store, counts, false)
	t.Cleanup(func() { _ = runtime.Close(context.Background()); _ = store.Close() })
	request := sqlIngestTurn(t)
	release := failSQLReceiptCursor(t, fixture)

	failures := 1
	if !reopen {
		failures = 2
	}

	for range failures {
		_, err := runtime.IngestSequencedTurn(t.Context(), request)
		if err == nil {
			t.Fatal("native receipt failure returned success")
		}

		assertSQLCanonicalCount(t, store, request.Turn.Scope, 1)
		assertSQLReceiptMissing(t, store, request.ProducerID, request.Turn.Scope)
	}

	release()

	proof := sqlOriginalTurnProof(t, store, request.Turn)

	id, err := memory.CanonicalSemanticOperationID(request.Turn.ExportID, memory.LegacyDerivationRef())
	if err != nil {
		t.Fatal(err)
	}

	operation, found, err := store.LoadCanonicalOperation(t.Context(), request.Turn.Scope, id)
	if err != nil || !found || len(operation.Outcome.RevisionIDs) != 1 {
		t.Fatalf("committed operation=%+v found=%t err=%v", operation, found, err)
	}

	if reopen {
		store = reopenSQLIngestStore(t, fixture, runtime, store)
		runtime = newSQLIngestRuntime(t, store, counts, false)
	}

	finishSQLTurnRecovery(t, runtime, store, counts, request, proof, operation)
}

func sqlOriginalTurnProof(t *testing.T, store sqlIngestStore, turn memory.Turn) memory.CanonicalTurnIdentityResult {
	t.Helper()

	proof, err := store.VerifyCanonicalTurn(t.Context(), turn)
	if err != nil || !proof.Found || !proof.FullInputVerified {
		t.Fatalf("committed original proof=%+v err=%v", proof, err)
	}

	return proof
}

func finishSQLTurnRecovery(t *testing.T, runtime *orderedingest.SequencedIngestRuntime,
	store sqlIngestStore, counts *sqlProcessingCounts, request memory.SequencedTurn,
	proof memory.CanonicalTurnIdentityResult, operation memory.CanonicalOperationRecord) {
	t.Helper()

	changed := request
	changed.Turn.Messages = append([]memory.Message(nil), request.Turn.Messages...)
	changed.Turn.Messages[0].Text += " changed"
	_, err := runtime.IngestSequencedTurn(t.Context(), changed)
	requireSQLCode(t, err, memory.CodeConflict, memory.ErrorClassPermanent)

	outcome, err := runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLSuccess(t, err)

	assertSQLRecoveredOutcome(t, outcome, 1, 1, memory.IngestDispositionAccepted)
	assertSQLCanonicalCount(t, store, request.Turn.Scope, 1)
	assertSQLTurnProofAndOperation(t, store, request, proof, operation)

	record, found, err := store.LoadIngestSequence(t.Context(), request.ProducerID, request.Turn.Scope, 1)
	if err != nil || !found || !reflect.DeepEqual(record.Outcome, outcome) {
		t.Fatalf("recovered durable receipt=%+v found=%t err=%v", record, found, err)
	}

	assertSQLTurnReplayAndNext(t, runtime, request)
	assertSQLCanonicalCount(t, store, request.Turn.Scope, 1)

	if counts.extraction != 1 {
		t.Fatalf("model extraction repeated: %+v", counts)
	}

	t.Logf("canonical effects=1 original proof/outcome preserved, extraction=%d processor attempts=%d "+
		"(retries run proof/outcome preflight only)", counts.extraction, counts.turnAttempts)
}

func assertSQLTurnProofAndOperation(t *testing.T, store sqlIngestStore, request memory.SequencedTurn,
	proof memory.CanonicalTurnIdentityResult, operation memory.CanonicalOperationRecord) {
	t.Helper()

	gotProof, err := store.VerifyCanonicalTurn(t.Context(), request.Turn)
	if err != nil || !reflect.DeepEqual(gotProof, proof) {
		t.Fatalf("recovery changed proof: err=%v", err)
	}

	id, err := memory.CanonicalSemanticOperationID(request.Turn.ExportID, memory.LegacyDerivationRef())
	requireSQLSuccess(t, err)

	gotOperation, found, err := store.LoadCanonicalOperation(t.Context(), request.Turn.Scope, id)
	if err != nil || !found || !reflect.DeepEqual(gotOperation, operation) {
		t.Fatalf("recovery changed original outcome: found=%t err=%v", found, err)
	}
}

func assertSQLTurnReplayAndNext(t *testing.T, runtime *orderedingest.SequencedIngestRuntime,
	request memory.SequencedTurn) {
	t.Helper()
	duplicate, err := runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLSuccess(t, err)
	assertSQLRecoveredOutcome(t, duplicate, 1, 1, memory.IngestDispositionDuplicate)
	// Turn receipts authorize authoritative normalized input, not the caller's
	// transport fingerprint. This preserves the original ordered-turn contract.
	request.Fingerprint = "transport-fingerprint-changed"
	if _, err := runtime.IngestSequencedTurn(t.Context(), request); err != nil {
		t.Fatal(err)
	}

	request.ScopeSequence = 3
	_, err = runtime.IngestSequencedTurn(t.Context(), request)
	requireSQLCode(t, err, memory.CodeConflict, memory.ErrorClassRetryable)

	request.ScopeSequence = 2
	if _, err := runtime.IngestSequencedTurn(t.Context(), request); err != nil {
		t.Fatal(err)
	}
}

func TestSQLOrderedBoundaryCommitBeforeReceiptRecovery(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store := fixture.Store.(sqlIngestStore)
		counts := &sqlProcessingCounts{}
		runtime := newSQLIngestRuntime(t, store, counts, true)
		t.Cleanup(func() { _ = runtime.Close(context.Background()); _ = store.Close() })
		request := sqlIngestBoundary(t)
		release := failSQLReceiptCursor(t, fixture)

		_, err := runtime.ApplySequencedBoundary(t.Context(), request)
		if err == nil {
			t.Fatal("native receipt failure returned success")
		}

		release()
		assertSQLCanonicalCount(t, store, request.Boundary.Scope, 2)
		assertSQLReceiptMissing(t, store, request.ProducerID, request.Boundary.Scope)
		originals := sqlBoundaryOperations(t, store, request.Boundary.Scope)

		proof, err := store.VerifyCanonicalBoundary(t.Context(), request.Boundary)
		if err != nil || !proof.Certified {
			t.Fatalf("boundary proof=%+v err=%v", proof, err)
		}

		store = reopenSQLIngestStore(t, fixture, runtime, store)
		runtime = newSQLIngestRuntime(t, store, counts, true)
		finishSQLBoundaryRecovery(t, runtime, store, request, originals)
		assertSQLCanonicalCount(t, store, request.Boundary.Scope, 2)

		if counts.scenario != 1 || counts.profile != 1 {
			t.Fatalf("boundary synthesis repeated: %+v", counts)
		}

		t.Logf("two canonical stage effects exactly once; original certificate/operation lineage; scenario=%d profile=%d",
			counts.scenario, counts.profile)
	})
}

func finishSQLBoundaryRecovery(t *testing.T, runtime *orderedingest.SequencedIngestRuntime,
	store sqlIngestStore, request memory.SequencedBoundary, originals map[string]memory.CanonicalOperationRecord) {
	t.Helper()

	changed := request
	changed.Boundary.Reason = memory.BoundaryReasonReset
	_, err := runtime.ApplySequencedBoundary(t.Context(), changed)
	requireSQLCode(t, err, memory.CodeConflict, memory.ErrorClassPermanent)

	accepted, err := runtime.ApplySequencedBoundary(t.Context(), request)
	requireSQLSuccess(t, err)

	assertSQLRecoveredOutcome(t, accepted, 1, 2, memory.IngestDispositionAccepted)

	if len(accepted.OperationIDs) != 2 {
		t.Fatalf("boundary receipt lost original stage lineage=%+v", accepted)
	}

	for _, id := range accepted.OperationIDs {
		operation, found, err := store.LoadCanonicalOperation(t.Context(), request.Boundary.Scope, id)
		if err != nil || !found || !reflect.DeepEqual(operation, originals[id]) {
			t.Fatalf("original boundary stage=%+v found=%t err=%v", operation, found, err)
		}
	}

	duplicate, err := runtime.ApplySequencedBoundary(t.Context(), request)
	requireSQLSuccess(t, err)

	assertSQLRecoveredOutcome(t, duplicate, 1, 2, memory.IngestDispositionDuplicate)

	request.Fingerprint = "changed"
	_, err = runtime.ApplySequencedBoundary(t.Context(), request)
	requireSQLCode(t, err, memory.CodeConflict, memory.ErrorClassPermanent)
}

func sqlBoundaryOperations(t *testing.T, store sqlIngestStore,
	scope memory.Scope) map[string]memory.CanonicalOperationRecord {
	t.Helper()

	changes, err := store.ScanScopeChanges(t.Context(), scope, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	result := make(map[string]memory.CanonicalOperationRecord, len(changes))
	for _, change := range changes {
		operation, found, err := store.LoadCanonicalOperation(t.Context(), scope, change.OperationID)
		if err != nil || !found {
			t.Fatalf("original boundary operation found=%t err=%v", found, err)
		}

		result[change.OperationID] = operation
	}

	return result
}

func assertSQLCanonicalCount(t *testing.T, store sqlIngestStore, scope memory.Scope, want uint64) {
	t.Helper()

	state, err := store.LoadScopeState(t.Context(), scope)
	if err != nil || state.Version != want || state.ChangeSeq != want {
		t.Fatalf("canonical state=%+v err=%v want=%d", state, err, want)
	}

	changes, err := store.ScanScopeChanges(t.Context(), scope, 0, 10)
	if err != nil || uint64(len(changes)) != want {
		t.Fatalf("canonical effects=%+v err=%v want=%d", changes, err, want)
	}
}

func assertSQLReceiptMissing(t *testing.T, store sqlIngestStore, producer string, scope memory.Scope) {
	t.Helper()

	_, found, err := store.LoadLatestIngestSequence(t.Context(), producer, scope)
	if err != nil || found {
		t.Fatalf("failed receipt advanced cursor found=%t err=%v", found, err)
	}

	_, found, err = store.LoadIngestSequence(t.Context(), producer, scope, 1)
	if err != nil || found {
		t.Fatalf("failed receipt retained row found=%t err=%v", found, err)
	}
}

func assertSQLRecoveredOutcome(t *testing.T, outcome memory.IngestOutcome, sequence, version uint64,
	disposition memory.IngestDisposition) {
	t.Helper()

	if outcome.ScopeSequence != sequence || outcome.ScopeVersion != version || outcome.ScopeChangeSeq != version ||
		outcome.Disposition != disposition {
		t.Fatalf("recovered outcome=%+v", outcome)
	}
}
