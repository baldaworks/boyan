package boundarycore

import "context"

import . "github.com/baldaworks/boyan/pkg/memory"

// ProcessBoundary synthesizes scenarios and then the exact-scope profile as
// two independently idempotent stages.
func (e *BoundaryProcessor) ProcessBoundary(ctx context.Context, boundary Boundary) (BoundaryOutcome, error) {
	partial := BoundaryOutcome{
		SchemaVersion: DerivedSchemaVersionV1,
		Scope:         boundary.Scope,
	}
	if e == nil {
		return partial, invalidDerived("boundary processor is required")
	}

	if err := checkBoundaryContext(ctx); err != nil {
		return partial, err
	}

	if err := boundary.Validate(); err != nil {
		return partial, err
	}

	scenarios, err := e.processScenarioStage(ctx, boundary)

	partial.Scenarios = scenarios
	if err != nil {
		return partial, err
	}

	profile, err := e.processProfileStage(ctx, boundary)

	partial.Profile = profile
	if err != nil {
		return partial, err
	}

	if err := partial.Validate(); err != nil {
		return partial, err
	}

	return partial, nil
}
