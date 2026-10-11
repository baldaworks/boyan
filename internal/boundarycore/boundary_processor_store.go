package boundarycore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (e *BoundaryProcessor) loadBoundarySnapshot(ctx context.Context, scope Scope) (ScopeSnapshot, error) {
	snapshot, err := e.store.LoadScope(ctx, scope)
	if err != nil {
		return ScopeSnapshot{}, boundaryStoreFailure(ctx, err)
	}

	if err := checkBoundaryContext(ctx); err != nil {
		return ScopeSnapshot{}, err
	}

	snapshot = cloneScopeSnapshot(snapshot)
	if err := snapshot.Validate(e.config.MaxSnapshotItems); err != nil {
		return ScopeSnapshot{}, err
	}

	if snapshot.Scope != scope {
		return ScopeSnapshot{}, PermanentError(CodeScopeViolation, "Store snapshot scope does not match the boundary", nil)
	}

	return snapshot, nil
}

func (e *BoundaryProcessor) commitBoundaryStage(ctx context.Context, request CommitRequest) (OperationOutcome, error) {
	if err := request.Validate(); err != nil {
		return OperationOutcome{}, err
	}

	outcome, err := e.store.Commit(ctx, cloneCommitRequest(request))
	if err != nil {
		return OperationOutcome{}, boundaryStoreFailure(ctx, err)
	}

	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, err
	}

	if err := validateCommittedOutcome(request, outcome); err != nil {
		return OperationOutcome{}, err
	}

	return cloneOperationOutcome(outcome), nil
}
