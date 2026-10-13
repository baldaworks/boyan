package boundarycore

import (
	"context"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (e *BoundaryProcessor) loadBoundarySnapshot(ctx context.Context,
	scope Scope,
	at time.Time) (BoundarySnapshot,
	error) {
	snapshot, err := e.store.LoadBoundarySnapshot(ctx, scope, at)
	if err != nil {
		return BoundarySnapshot{}, boundaryStoreFailure(ctx, err)
	}

	if err := checkBoundaryContext(ctx); err != nil {
		return BoundarySnapshot{}, err
	}

	snapshot = cloneBoundarySnapshot(snapshot)
	if err := snapshot.Validate(e.config.MaxSnapshotItems, at); err != nil {
		return BoundarySnapshot{}, err
	}

	if snapshot.Structural.Scope != scope {
		return BoundarySnapshot{}, PermanentError(CodeScopeViolation, "Store snapshot scope does not match the boundary", nil)
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
