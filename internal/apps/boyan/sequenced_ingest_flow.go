package app

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (r *SequencedIngestRuntime) IngestSequencedTurn(ctx context.Context, request memory.SequencedTurn) (memory.IngestOutcome, error) {
	if r == nil || r.turn.processor == nil {
		return memory.IngestOutcome{}, memory.PermanentError(memory.CodeDisabled, "sequenced turn ingestion is unavailable", nil)
	}
	if err := request.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}
	var outcome memory.IngestOutcome
	err := r.lanes.Execute(ctx, request.Turn.Scope, func(runCtx context.Context) error {
		var ingestErr error
		outcome, ingestErr = r.ingestTurn(runCtx, request)
		return ingestErr
	})
	return outcome, err
}

func (r *SequencedIngestRuntime) ApplySequencedBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error) {
	if r == nil || r.boundary.processor == nil {
		return memory.IngestOutcome{}, memory.PermanentError(memory.CodeDisabled, "sequenced boundary ingestion is unavailable", nil)
	}
	if err := request.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}
	var outcome memory.IngestOutcome
	err := r.lanes.Execute(ctx, request.Boundary.Scope, func(runCtx context.Context) error {
		var ingestErr error
		outcome, ingestErr = r.ingestBoundary(runCtx, request)
		return ingestErr
	})
	return outcome, err
}

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

func (r *SequencedIngestRuntime) ingestBoundary(ctx context.Context, request memory.SequencedBoundary) (memory.IngestOutcome, error) {
	if replay, ok, err := r.loadReplay(ctx, request.ProducerID, request.Boundary.Scope, request.ScopeSequence, request.Fingerprint); ok || err != nil {
		return replay, err
	}
	boundaryOutcome, err := r.boundary.processor.ProcessBoundary(ctx, request.Boundary)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	state, err := r.canonical.LoadScopeState(ctx, request.Boundary.Scope)
	if err != nil {
		return memory.IngestOutcome{}, err
	}
	operationIDs := make([]string, 0, 2)
	if boundaryOutcome.Scenarios.OperationID != "" {
		operationIDs = append(operationIDs, boundaryOutcome.Scenarios.OperationID)
	}
	if boundaryOutcome.Profile.OperationID != "" {
		operationIDs = append(operationIDs, boundaryOutcome.Profile.OperationID)
	}
	outcome := memory.IngestOutcome{
		SchemaVersion:  memory.IngestOutcomeSchemaVersionV1,
		Kind:           memory.IngestKindBoundary,
		Disposition:    memory.IngestDispositionAccepted,
		Projection:     memory.ProjectionStatusCurrent,
		ProducerID:     request.ProducerID,
		Scope:          request.Boundary.Scope,
		ScopeSequence:  request.ScopeSequence,
		Fingerprint:    request.Fingerprint,
		ExportID:       request.Boundary.ExportID,
		OperationIDs:   operationIDs,
		ScopeVersion:   state.Version,
		ScopeChangeSeq: state.ChangeSeq,
	}
	if err := outcome.Validate(); err != nil {
		return memory.IngestOutcome{}, err
	}
	outcome = r.markProjectionStatus(ctx, request.Boundary.Scope, outcome, r.boundary.projection, r.boundary.repair)
	if err := r.sequences.SaveIngestSequence(ctx, memory.IngestSequenceRecord{
		SchemaVersion: memory.IngestSequenceSchemaVersionV1,
		ProducerID:    request.ProducerID,
		Scope:         request.Boundary.Scope,
		Sequence:      request.ScopeSequence,
		Fingerprint:   request.Fingerprint,
		Outcome:       outcome,
	}); err != nil {
		return memory.IngestOutcome{}, err
	}
	return outcome, nil
}

func (r *SequencedIngestRuntime) loadReplay(ctx context.Context, producerID string, scope memory.Scope, sequence uint64, fingerprint string) (memory.IngestOutcome, bool, error) {
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
			return memory.IngestOutcome{}, true, memory.PermanentError(memory.CodeConflict, "ingest sequence fingerprint conflicted", nil)
		}
		outcome := record.Outcome
		outcome.Disposition = memory.IngestDispositionDuplicate
		return outcome, true, nil
	}
}

func (r *SequencedIngestRuntime) markProjectionStatus(ctx context.Context, scope memory.Scope, outcome memory.IngestOutcome, projection ProjectionSyncer, repair *projectionRepairer) memory.IngestOutcome {
	if projection == nil {
		return outcome
	}
	if err := projection.Sync(ctx, scope); err != nil {
		outcome.Projection = memory.ProjectionStatusLagging
		if repair != nil {
			_ = repair.Mark(scope)
		}
	}
	return outcome
}
