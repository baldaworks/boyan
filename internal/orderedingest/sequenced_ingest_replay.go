package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) loadReplay(
	ctx context.Context,
	producerID string,
	scope memory.Scope,
	sequence uint64,
	fingerprint string,
) (memory.IngestOutcome, bool, error) {
	record, found, err := r.loadReplayRecord(ctx, producerID, scope, sequence)
	if err != nil || !found {
		return memory.IngestOutcome{}, found, err
	}

	if record.Fingerprint != fingerprint {
		return memory.IngestOutcome{}, true,
			memory.PermanentError(memory.CodeConflict, "ingest sequence fingerprint conflicted", nil)
	}

	return duplicateReplayOutcome(record), true, nil
}

func (r *SequencedIngestRuntime) loadReplayRecord(
	ctx context.Context, producerID string, scope memory.Scope, sequence uint64,
) (memory.IngestSequenceRecord, bool, error) {
	cursor, found, err := r.sequences.LoadLatestIngestSequence(ctx, producerID, scope)
	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}

	if found {
		if err := validateReplayCursor(cursor, producerID, scope); err != nil {
			return memory.IngestSequenceRecord{}, true, err
		}
	}

	next := uint64(1)
	if found {
		next = cursor.Latest + 1
	}

	switch {
	case sequence > next:
		return memory.IngestSequenceRecord{}, true, memory.RetryableError(memory.CodeConflict, "ingest sequence gap", nil)
	case sequence == next:
		return memory.IngestSequenceRecord{}, false, nil
	default:
		return r.loadStoredReplayRecord(ctx, producerID, scope, sequence)
	}
}

func (r *SequencedIngestRuntime) loadStoredReplayRecord(
	ctx context.Context, producerID string, scope memory.Scope, sequence uint64,
) (memory.IngestSequenceRecord, bool, error) {
	record, recordFound, err := r.sequences.LoadIngestSequence(ctx, producerID, scope, sequence)
	if err != nil {
		return memory.IngestSequenceRecord{}, true, err
	}

	if !recordFound {
		return memory.IngestSequenceRecord{}, true,
			memory.PermanentError(memory.CodeConflict, "ingest sequence regressed", nil)
	}

	if err := validateReplayRecord(record, producerID, scope, sequence); err != nil {
		return memory.IngestSequenceRecord{}, true, err
	}

	return record, true, nil
}

func validateReplayRecord(record memory.IngestSequenceRecord, producerID string,
	scope memory.Scope, sequence uint64,
) error {
	if err := record.Validate(); err != nil {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest receipt is invalid", err)
	}

	if record.ProducerID != producerID || record.Scope != scope || record.Sequence != sequence {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest receipt ownership differs", nil)
	}

	return nil
}

func duplicateReplayOutcome(record memory.IngestSequenceRecord) memory.IngestOutcome {
	outcome := record.Outcome
	outcome.Disposition = memory.IngestDispositionDuplicate

	return outcome
}

func validateReplayCursor(cursor memory.IngestSequenceCursor, producerID string, scope memory.Scope) error {
	if err := cursor.Validate(); err != nil {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest cursor is invalid", err)
	}

	if cursor.ProducerID != producerID || cursor.Scope != scope {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest cursor ownership differs", nil)
	}

	return nil
}
