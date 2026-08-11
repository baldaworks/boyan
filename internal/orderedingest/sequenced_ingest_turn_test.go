package orderedingest

import (
	"context"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestSequencedIngestRuntimeAcceptsAndReplaysTurn(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	request := testSequencedTurn(t, 1, "fingerprint-1")
	first, err := runtime.IngestSequencedTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("IngestSequencedTurn(first) error = %v", err)
	}
	if first.Disposition != boyan.IngestDispositionAccepted || first.Projection != boyan.ProjectionStatusCurrent || first.ScopeVersion != 1 || first.ScopeChangeSeq != 1 {
		t.Fatalf("first outcome = %+v, want accepted/current with canonical watermark", first)
	}

	second, err := runtime.IngestSequencedTurn(context.Background(), request)
	if err != nil {
		t.Fatalf("IngestSequencedTurn(replay) error = %v", err)
	}
	if second.Disposition != boyan.IngestDispositionDuplicate || second.ScopeSequence != 1 || second.Fingerprint != request.Fingerprint {
		t.Fatalf("duplicate outcome = %+v, want duplicate replay for same sequence/fingerprint", second)
	}
}

func TestSequencedIngestRuntimeRejectsGapAndFingerprintConflict(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	gapRequest := testSequencedTurn(t, 2, "fingerprint-gap")
	if _, err := runtime.IngestSequencedTurn(context.Background(), gapRequest); !hasCodeClass(err, boyan.CodeConflict, boyan.ErrorClassRetryable) {
		t.Fatalf("gap error = %v, want retryable conflict", err)
	}

	first := testSequencedTurn(t, 1, "fingerprint-1")
	if _, err := runtime.IngestSequencedTurn(context.Background(), first); err != nil {
		t.Fatalf("IngestSequencedTurn(first) error = %v", err)
	}
	conflict := testSequencedTurn(t, 1, "fingerprint-conflict")
	if _, err := runtime.IngestSequencedTurn(context.Background(), conflict); !hasCodeClass(err, boyan.CodeConflict, boyan.ErrorClassPermanent) {
		t.Fatalf("conflict error = %v, want permanent conflict", err)
	}
}

func TestSequencedIngestRuntimeCloseRejectsNewWork(t *testing.T) {
	t.Parallel()

	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, fakeTurnProcessor{outcome: boyan.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}}, boyan.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
	if err != nil {
		t.Fatalf("NewSequencedIngestRuntime() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := runtime.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := runtime.IngestSequencedTurn(context.Background(), testSequencedTurn(t, 1, "fingerprint-close")); !hasCodeClass(err, boyan.CodeShuttingDown, boyan.ErrorClassPermanent) {
		t.Fatalf("IngestSequencedTurn(after close) error = %v, want shutting_down", err)
	}
}
