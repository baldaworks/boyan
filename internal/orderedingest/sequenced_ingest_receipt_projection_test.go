package orderedingest //nolint:testpackage // Exercises receipt failure ordering with projection repair.

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type failingReceiptStore struct {
	*memoryIngestSequenceStore

	failNext bool
}

func (s *failingReceiptStore) SaveIngestSequence(ctx context.Context, record memory.IngestSequenceRecord) error {
	if s.failNext {
		s.failNext = false

		return memory.RetryableError(memory.CodeStoreFailure, "receipt save failed", nil)
	}

	return s.memoryIngestSequenceStore.SaveIngestSequence(ctx, record)
}

func TestReceiptSaveFailureStillSchedulesProjectionRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	request := testSequencedTurn(t, 1, "receipt-projection")
	sequences := &failingReceiptStore{memoryIngestSequenceStore: newMemoryIngestSequenceStore(), failNext: true}
	projection := &flakyProjectionSyncer{failuresRemaining: 1, synced: make(chan struct{}, 2)}
	processor := newIdentityTurnProcessor(t, request.Turn, memory.CanonicalMutationOutcome{
		ScopeVersion: 7, ChangeSeq: 9,
	})
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, processor,
		memory.LegacyDerivationRef(), projection, nil, nil,
		SequencedIngestConfig{ProjectionRetryDelay: time.Millisecond})
	requireNoError(t, err, "construct runtime: %v")
	requireNoError(t, runtime.Start(ctx), "start runtime: %v")

	t.Cleanup(func() { _ = runtime.Close(ctx) })

	_, err = runtime.IngestSequencedTurn(ctx, request)
	if !hasCodeClass(err, memory.CodeStoreFailure, memory.ErrorClassRetryable) {
		t.Fatalf("first ingest error = %v, want receipt failure", err)
	}

	waitForProjectionSync(t, projection, 2)

	_, found, err := sequences.LoadLatestIngestSequence(ctx, request.ProducerID, request.Turn.Scope)
	requireNoError(t, err, "load cursor: %v")

	if found {
		t.Fatal("failed receipt save advanced sequence")
	}

	accepted, err := runtime.IngestSequencedTurn(ctx, request)
	requireNoError(t, err, "retry ingest: %v")

	if accepted.Disposition != memory.IngestDispositionAccepted ||
		accepted.Projection != memory.ProjectionStatusCurrent || accepted.ScopeChangeSeq != 9 {
		t.Fatalf("retry outcome = %+v, want accepted/current original change sequence", accepted)
	}

	duplicate, err := runtime.IngestSequencedTurn(ctx, request)
	requireNoError(t, err, "duplicate ingest: %v")

	if duplicate.Disposition != memory.IngestDispositionDuplicate || duplicate.ScopeChangeSeq != 9 {
		t.Fatalf("duplicate outcome = %+v, want duplicate with original change sequence", duplicate)
	}
}
