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
	cursor, found, err := r.sequences.LoadLatestIngestSequence(ctx, producerID, scope)
	if err != nil {
		return memory.IngestOutcome{}, false, err
	}

	next := uint64(1)
	if found {
		next = cursor.Latest + 1
	}

	switch {
	case sequence > next:
		return memory.IngestOutcome{}, true, memory.RetryableError(memory.CodeConflict, "ingest sequence gap", nil)
	case sequence == next:
		return memory.IngestOutcome{}, false, nil
	default:
		record, recordFound, err := r.sequences.LoadIngestSequence(ctx, producerID, scope, sequence)
		if err != nil {
			return memory.IngestOutcome{}, true, err
		}

		if !recordFound {
			return memory.IngestOutcome{}, true, memory.PermanentError(memory.CodeConflict, "ingest sequence regressed", nil)
		}

		if record.Fingerprint != fingerprint {
			return memory.IngestOutcome{}, true, memory.PermanentError(
				memory.CodeConflict,
				"ingest sequence fingerprint conflicted",
				nil,
			)
		}

		outcome := record.Outcome
		outcome.Disposition = memory.IngestDispositionDuplicate

		return outcome, true, nil
	}
}
