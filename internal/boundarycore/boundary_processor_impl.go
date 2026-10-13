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

	if verifier, ok := e.store.(CanonicalBoundaryIdentityStore); ok {
		if _, err := verifier.VerifyCanonicalBoundary(ctx, boundary); err != nil {
			return partial, err
		}
	}

	at := e.clock().UTC()
	if at.IsZero() {
		return partial, invalidDerived("boundary processing time is required")
	}

	scenarios, err := e.processScenarioStage(ctx, boundary, at)

	partial.Scenarios = scenarios
	if err != nil {
		return partial, err
	}

	profile, err := e.processProfileStage(ctx, boundary, at)

	partial.Profile = profile
	if err != nil {
		return partial, err
	}

	if err := partial.Validate(); err != nil {
		return partial, err
	}

	return partial, nil
}
