package app

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

func (e *BoundaryProcessor) processScenarioStage(ctx context.Context, boundary Boundary) (OperationOutcome, error) {
	operationID, err := ProcessingOperationID(OperationStageScenarios, boundary.ExportID, e.config.Derivation)
	if err != nil {
		return OperationOutcome{}, err
	}
	lookup := OperationLookup{
		SchemaVersion: DerivedSchemaVersionV1,
		OperationID:   operationID,
		Stage:         OperationStageScenarios,
		Scope:         boundary.Scope,
	}
	prior, err := e.store.LookupOperation(ctx, lookup)
	if err != nil {
		return OperationOutcome{}, boundaryStoreFailure(ctx, err)
	}
	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, err
	}
	if err := prior.Validate(lookup); err != nil {
		return OperationOutcome{}, err
	}
	if prior.Found {
		return cloneOperationOutcome(prior.Outcome), nil
	}

	snapshot, err := e.loadBoundarySnapshot(ctx, boundary.Scope)
	if err != nil {
		return OperationOutcome{}, err
	}
	candidates, err := e.scenarioSynthesizer.SynthesizeScenarios(ctx, ScenarioSynthesisRequest{
		SchemaVersion: DerivedSchemaVersionV1,
		Derivation:    e.config.Derivation,
		Boundary:      boundary,
		View:          scopeViewFromSnapshot(snapshot),
	})
	if err != nil {
		return OperationOutcome{}, boundaryModelFailure(ctx, err)
	}
	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, err
	}
	if len(candidates) > e.config.MaxCandidateCount {
		return OperationOutcome{}, limitExceeded("scenario synthesis exceeds the candidate limit")
	}
	scenarios, transitions, err := e.groundScenarioCandidates(boundary, operationID, snapshot, candidates)
	if err != nil {
		return OperationOutcome{}, err
	}
	commit := CommitRequest{
		SchemaVersion:        DerivedSchemaVersionV1,
		OperationID:          operationID,
		Stage:                OperationStageScenarios,
		Scope:                boundary.Scope,
		ExpectedScopeVersion: snapshot.Version,
		Scenarios:            scenarios,
		Transitions:          transitions,
	}
	return e.commitBoundaryStage(ctx, commit)
}

func (e *BoundaryProcessor) processProfileStage(ctx context.Context, boundary Boundary) (OperationOutcome, error) {
	operationID, err := ProcessingOperationID(OperationStageProfile, boundary.ExportID, e.config.Derivation)
	if err != nil {
		return OperationOutcome{}, err
	}
	lookup := OperationLookup{
		SchemaVersion: DerivedSchemaVersionV1,
		OperationID:   operationID,
		Stage:         OperationStageProfile,
		Scope:         boundary.Scope,
	}
	prior, err := e.store.LookupOperation(ctx, lookup)
	if err != nil {
		return OperationOutcome{}, boundaryStoreFailure(ctx, err)
	}
	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, err
	}
	if err := prior.Validate(lookup); err != nil {
		return OperationOutcome{}, err
	}
	if prior.Found {
		return cloneOperationOutcome(prior.Outcome), nil
	}

	snapshot, err := e.loadBoundarySnapshot(ctx, boundary.Scope)
	if err != nil {
		return OperationOutcome{}, err
	}
	candidate, err := e.profileSynthesizer.SynthesizeProfile(ctx, ProfileSynthesisRequest{
		SchemaVersion: DerivedSchemaVersionV1,
		Derivation:    e.config.Derivation,
		Boundary:      boundary,
		View:          scopeViewFromSnapshot(snapshot),
	})
	if err != nil {
		return OperationOutcome{}, boundaryModelFailure(ctx, err)
	}
	if err := checkBoundaryContext(ctx); err != nil {
		return OperationOutcome{}, err
	}
	profiles, transitions, err := e.groundProfileCandidate(boundary, operationID, snapshot, candidate)
	if err != nil {
		return OperationOutcome{}, err
	}
	commit := CommitRequest{
		SchemaVersion:        DerivedSchemaVersionV1,
		OperationID:          operationID,
		Stage:                OperationStageProfile,
		Scope:                boundary.Scope,
		ExpectedScopeVersion: snapshot.Version,
		Profiles:             profiles,
		Transitions:          transitions,
	}
	return e.commitBoundaryStage(ctx, commit)
}

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
