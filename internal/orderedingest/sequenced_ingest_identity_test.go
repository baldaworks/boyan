package orderedingest //nolint:testpackage // Exercises receipt replay authorization with portable processors.

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestTurnReceiptReplayRequiresAuthoritativeIdentityCapability(t *testing.T) {
	runtime, err := newSequencedIngestRuntimeWithParts(newMemoryIngestSequenceStore(), fakeCanonicalStore{},
		fakeTurnProcessor{outcome: memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}},
		memory.LegacyDerivationRef(),
		nil, nil, nil, SequencedIngestConfig{})
	requireNoError(t, err, "runtime: %v")
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	request := testSequencedTurn(t, 1, "delivery-fingerprint")
	_, err = runtime.IngestSequencedTurn(context.Background(), request)
	requireNoError(t, err, "initial ingest: %v")

	_, err = runtime.IngestSequencedTurn(context.Background(), request)
	if !hasCodeClass(err, memory.CodeConflict, memory.ErrorClassPermanent) {
		t.Fatalf("unsupported replay=%v", err)
	}
}

type boundaryProofCanonical struct {
	CanonicalStateReader

	deny bool
}

func (s *boundaryProofCanonical) VerifyCanonicalBoundary(context.Context,
	memory.Boundary) (memory.BoundaryIdentityResult, error) {
	if s.deny {
		return memory.BoundaryIdentityResult{}, memory.PermanentError(memory.CodeConflict, "changed boundary", nil)
	}

	return memory.BoundaryIdentityResult{SafelyNew: true}, nil
}

func TestBoundaryProofRunsBeforeReceiptReplay(t *testing.T) {
	request := testSequencedBoundary(t, 1, "supplied-fingerprint")
	runtime := newBoundaryReplayTestRuntime(t, request.Boundary.Scope, newMemoryIngestSequenceStore())
	proof := &boundaryProofCanonical{CanonicalStateReader: runtime.canonical}
	runtime.canonical = proof
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	_, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "first: %v")

	proof.deny = true
	request.Boundary.Reason = memory.BoundaryReasonReset

	_, err = runtime.ApplySequencedBoundary(context.Background(), request)
	if !hasCodeClass(err, memory.CodeConflict, memory.ErrorClassPermanent) {
		t.Fatalf("receipt bypassed proof: %v", err)
	}
}

type proofChangingReceiptStore struct {
	*memoryIngestSequenceStore

	proof *boundaryProofCanonical
}

func (s *proofChangingReceiptStore) LoadIngestSequence(ctx context.Context, producer string, scope memory.Scope,
	sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	record, found, err := s.memoryIngestSequenceStore.LoadIngestSequence(ctx, producer, scope, sequence)
	s.proof.deny = true

	return record, found, err
}

func TestBoundaryProofRechecksAfterReceiptRead(t *testing.T) {
	request := testSequencedBoundary(t, 1, "supplied-fingerprint")
	sequences := newMemoryIngestSequenceStore()
	runtime := newBoundaryReplayTestRuntime(t, request.Boundary.Scope, sequences)
	proof := &boundaryProofCanonical{CanonicalStateReader: runtime.canonical}
	runtime.canonical = proof
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	_, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "first: %v")

	runtime.sequences = &proofChangingReceiptStore{memoryIngestSequenceStore: sequences, proof: proof}

	_, err = runtime.ApplySequencedBoundary(context.Background(), request)
	if !hasCodeClass(err, memory.CodeConflict, memory.ErrorClassPermanent) {
		t.Fatalf("receipt read raced proof: %v", err)
	}
}

func TestBoundaryReceiptRequiresCertifiedProofForCapableStore(t *testing.T) {
	request := testSequencedBoundary(t, 1, "supplied-fingerprint")
	runtime := newBoundaryReplayTestRuntime(t, request.Boundary.Scope, newMemoryIngestSequenceStore())
	runtime.canonical = &boundaryProofCanonical{CanonicalStateReader: runtime.canonical}
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	_, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "initial fixture: %v")

	_, err = runtime.ApplySequencedBoundary(context.Background(), request)
	if !hasCodeClass(err, memory.CodeConflict, memory.ErrorClassPermanent) {
		t.Fatalf("uncertified receipt replay=%v", err)
	}
}

func TestBoundaryReceiptRejectsUnsupportedProofCapability(t *testing.T) {
	request := testSequencedBoundary(t, 1, "supplied-fingerprint")
	runtime := newBoundaryReplayTestRuntime(t, request.Boundary.Scope, newMemoryIngestSequenceStore())
	runtime.canonical = runtime.canonical.(boundaryFixtureCanonical).fakeCanonicalStore
	requireNoError(t, runtime.Start(context.Background()), "start: %v")
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	_, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "fresh portable processor: %v")

	_, err = runtime.ApplySequencedBoundary(context.Background(), request)
	if !hasCodeClass(err, memory.CodeConflict, memory.ErrorClassPermanent) {
		t.Fatalf("unsupported proof replay=%v", err)
	}
}
