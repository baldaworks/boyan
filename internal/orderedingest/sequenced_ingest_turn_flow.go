package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) ingestTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error) {
	if replay, ok, err := r.loadReplay(ctx, request.ProducerID, request.Turn.Scope, request.ScopeSequence, request.Fingerprint); ok || err != nil {
		return replay, err
	}
	canonicalOutcome, err := r.turn.processor.ProcessTurn(ctx, request.Turn, r.turn.derivation)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	operationID, err := memory.CanonicalSemanticOperationID(request.Turn.ExportID, r.turn.derivation)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	outcome := memory.IngestOutcome{
		SchemaVersion:  memory.IngestOutcomeSchemaVersionV1,
		Kind:           memory.IngestKindTurn,
		Disposition:    memory.IngestDispositionAccepted,
		Projection:     memory.ProjectionStatusCurrent,
		ProducerID:     request.ProducerID,
		Scope:          request.Turn.Scope,
		ScopeSequence:  request.ScopeSequence,
		Fingerprint:    request.Fingerprint,
		ExportID:       request.Turn.ExportID,
		OperationIDs:   []string{operationID},
		ScopeVersion:   canonicalOutcome.ScopeVersion,
		ScopeChangeSeq: canonicalOutcome.ChangeSeq,
	}
	if err := outcome.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}
	outcome = r.markProjectionStatus(ctx, request.Turn.Scope, outcome, r.turn.projection, r.turn.repair)
	if err := r.sequences.SaveIngestSequence(ctx, memory.IngestSequenceRecord{
		SchemaVersion: memory.IngestSequenceSchemaVersionV1,
		ProducerID:    request.ProducerID,
		Scope:         request.Turn.Scope,
		Sequence:      request.ScopeSequence,
		Fingerprint:   request.Fingerprint,
		Outcome:       outcome,
	}); err != nil {
		return memory.IngestOutcome{}, err
	}
	return outcome, nil
}
