package orderedingest //nolint:testpackage // Verifies portable receipt locator ownership before replay.

import (
	"context"
	"reflect"
	"sync/atomic"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type substitutedTurnReceiptStore struct {
	*memoryIngestSequenceStore

	record memory.IngestSequenceRecord
}

func (s *substitutedTurnReceiptStore) LoadIngestSequence(
	context.Context, string, memory.Scope, uint64,
) (memory.IngestSequenceRecord, bool, error) {
	return s.record, true, nil
}

func TestTurnReplayRejectsUnownedOrMalformedPortableReceipt(t *testing.T) {
	for _, kind := range []string{"producer", "sequence", "schema"} {
		t.Run(kind, func(t *testing.T) {
			request := testSequencedTurn(t, 1, "matching-delivery-fingerprint")
			sequences := newMemoryIngestSequenceStore()

			var calls atomic.Int32

			processor := newIdentityTurnProcessor(t, request.Turn,
				memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1})
			processor.process = func(
				context.Context, memory.Turn, memory.DerivationRef,
			) (memory.CanonicalMutationOutcome, error) {
				calls.Add(1)

				return memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}, nil
			}
			runtime, err := newSequencedIngestRuntimeWithParts(sequences, fakeCanonicalStore{}, processor,
				memory.LegacyDerivationRef(), nil, nil, nil, SequencedIngestConfig{})
			requireNoError(t, err, "runtime: %v")
			requireNoError(t, runtime.Start(context.Background()), "start: %v")
			t.Cleanup(func() { _ = runtime.Close(context.Background()) })

			_, err = runtime.IngestSequencedTurn(context.Background(), request)
			requireNoError(t, err, "first ingest: %v")
			record, _, err := sequences.LoadIngestSequence(context.Background(), request.ProducerID,
				request.Turn.Scope, request.ScopeSequence)
			requireNoError(t, err, "receipt: %v")

			record = substituteTurnReceiptLocator(record, kind)
			runtime.sequences = &substitutedTurnReceiptStore{memoryIngestSequenceStore: sequences, record: record}

			outcome, err := runtime.IngestSequencedTurn(context.Background(), request)
			if err == nil || !reflect.DeepEqual(outcome, memory.IngestOutcome{}) || calls.Load() != 1 {
				t.Fatalf("unowned/malformed receipt replayed: outcome=%+v error=%v processCalls=%d", outcome, err, calls.Load())
			}
		})
	}
}

func substituteTurnReceiptLocator(record memory.IngestSequenceRecord, kind string) memory.IngestSequenceRecord {
	switch kind {
	case "producer":
		record.ProducerID += "-other"
		record.Outcome.ProducerID = record.ProducerID
	case "sequence":
		record.Sequence++
		record.Outcome.ScopeSequence = record.Sequence
	case "schema":
		record.SchemaVersion = "unsupported"
	}

	return record
}
