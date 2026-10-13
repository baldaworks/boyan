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
