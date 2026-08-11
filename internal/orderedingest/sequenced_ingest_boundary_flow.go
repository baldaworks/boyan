package orderedingest

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

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
