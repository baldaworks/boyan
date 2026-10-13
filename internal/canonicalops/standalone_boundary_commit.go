package canonicalops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalBoundaryStore) Commit(
	ctx context.Context,
	request memory.CommitRequest,
) (
	memory.OperationOutcome,
	error,
) {
	if err := request.Validate(); err != nil {
		return memory.OperationOutcome{}, err
	}

	if request.ProcessingAt.IsZero() {
		return memory.OperationOutcome{},
			memory.PermanentError(memory.CodeInvalidDerived, "boundary processing time is required", nil)
	}

	if len(request.Scenarios)+len(request.Profiles) == 0 {
		return s.commitCanonicalOperation(ctx, request)
	}

	view, err := s.reader.LoadCanonicalBoundaryViewAt(ctx, request.Scope, request.ProcessingAt)
	if err != nil {
		return memory.OperationOutcome{}, err
	}

	if err := validateBoundaryGroundingAt(request, view.Boundary); err != nil {
		return memory.OperationOutcome{}, err
	}

	mutation, err := s.buildBoundaryMutation(ctx, request, view)
	if err != nil {
		return memory.OperationOutcome{}, err
	}

	outcome, err := s.store.ApplyCanonicalMutation(ctx, mutation)
	if err != nil {
		return memory.OperationOutcome{}, err
	}

	return memory.OperationOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
		Revisions:     collectOperationRevisions(request),
	}, nil
}

func (s *CanonicalBoundaryStore) commitCanonicalOperation(
	ctx context.Context,
	request memory.CommitRequest,
) (
	memory.OperationOutcome,
	error,
) {
	committedAt := operationCommittedAt(request)

	outcome, err := s.store.CommitCanonicalOperation(ctx, memory.CanonicalOperationCommitRequest{
		Scope:                request.Scope,
		ExpectedScopeVersion: request.ExpectedScopeVersion,
		OperationID:          request.OperationID,
		Fingerprint:          stableCanonicalFingerprint("boundary-operation", request),
		CommittedAt:          committedAt,
	})
	if err != nil {
		return memory.OperationOutcome{}, err
	}

	return memory.OperationOutcome{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		OperationID:   request.OperationID,
		Stage:         request.Stage,
		Scope:         request.Scope,
		ScopeVersion:  outcome.ScopeVersion,
	}, nil
}
