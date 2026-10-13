package boundarycore

import (
	"context"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (e *BoundaryProcessor) prepareBoundaryStage(
	ctx context.Context,
	boundary Boundary,
	stage OperationStage,
	at time.Time,
) (string, BoundarySnapshot, OperationOutcome, bool, error) {
	operationID, err := ProcessingOperationID(stage, boundary.ExportID, e.config.Derivation)
	if err != nil {
		return "", BoundarySnapshot{}, OperationOutcome{}, false, err
	}

	prior, found, err := e.lookupBoundaryStage(ctx, boundary.Scope, stage, operationID)
	if err != nil || found {
		return operationID, BoundarySnapshot{}, prior, found, err
	}

	snapshot, err := e.loadBoundarySnapshot(ctx, boundary.Scope, at)
	if err != nil {
		return "", BoundarySnapshot{}, OperationOutcome{}, false, err
	}

	return operationID, snapshot, OperationOutcome{}, false, nil
}

func (e *BoundaryProcessor) lookupBoundaryStage(
	ctx context.Context,
	scope Scope,
	stage OperationStage,
	operationID string,
) (OperationOutcome, bool, error) {
	lookup := OperationLookup{
		SchemaVersion: DerivedSchemaVersionV1,
		OperationID:   operationID,
		Stage:         stage,
		Scope:         scope,
	}

	prior, err := e.store.LookupOperation(ctx, lookup)
	if err != nil {
		return OperationOutcome{}, false, boundaryStoreFailure(ctx, err)
	}

	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, false, err
	}

	if err := prior.Validate(lookup); err != nil {
		return OperationOutcome{}, false, err
	}

	if prior.Found {
		return cloneOperationOutcome(prior.Outcome), true, nil
	}

	return OperationOutcome{}, false, nil
}
