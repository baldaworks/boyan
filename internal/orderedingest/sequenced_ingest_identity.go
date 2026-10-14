package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) verifyTurnReceipt(
	ctx context.Context,
	turn memory.Turn,
	outcome memory.IngestOutcome,
) error {
	if outcome.Kind != memory.IngestKindTurn || outcome.ExportID != turn.ExportID || outcome.Scope != turn.Scope {
		return memory.PermanentError(memory.CodeConflict, "ingest receipt turn identity differs", nil)
	}

	verifier, supported := r.turn.processor.(memory.CanonicalTurnIdentityStore)
	if !supported {
		return memory.PermanentError(memory.CodeConflict, "ingest receipt identity cannot be certified", nil)
	}

	proof, err := verifier.VerifyCanonicalTurn(ctx, turn)
	if err != nil {
		return err
	}

	return validateTurnReceiptProof(turn, proof)
}

func validateTurnReceiptProof(turn memory.Turn, proof memory.CanonicalTurnIdentityResult) error {
	if !proof.Found {
		return memory.PermanentError(memory.CodeConflict, "ingest receipt canonical source is unavailable", nil)
	}

	if proof.Source.Scope != turn.Scope || proof.Source.SourceID != memory.TurnSourceID(turn.ExportID) {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest identity proof source differs", nil)
	}

	if err := proof.Source.Validate(); err != nil {
		return memory.PermanentError(memory.CodeStoreFailure, "ingest identity proof source is invalid", err)
	}

	if !proof.FullInputVerified {
		return memory.PermanentError(memory.CodeConflict, "ingest receipt full input proof is unavailable", nil)
	}

	return nil
}

func (r *SequencedIngestRuntime) loadTurnReplay(
	ctx context.Context,
	request memory.SequencedTurn,
) (memory.IngestOutcome, bool, error) {
	record, found, err := r.loadReplayRecord(
		ctx, request.ProducerID, request.Turn.Scope, request.ScopeSequence,
	)
	if err != nil || !found {
		return memory.IngestOutcome{}, found, err
	}

	if err := r.verifyTurnReceipt(ctx, request.Turn, record.Outcome); err != nil {
		return memory.IngestOutcome{}, true, err
	}

	return duplicateReplayOutcome(record), true, nil
}
