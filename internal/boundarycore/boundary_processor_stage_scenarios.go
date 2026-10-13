package boundarycore

import (
	"context"
	"time"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (e *BoundaryProcessor) processScenarioStage(ctx context.Context,
	boundary Boundary,
	at time.Time) (OperationOutcome,
	error) {
	operationID, snapshot, prior, found, err := e.prepareBoundaryStage(ctx, boundary, OperationStageScenarios, at)
	if err != nil || found {
		return prior, err
	}

	candidates, err := e.scenarioSynthesizer.SynthesizeScenarios(ctx, ScenarioSynthesisRequest{
		SchemaVersion: DerivedSchemaVersionV1,
		Derivation:    e.config.Derivation,
		Boundary:      boundary,
		View:          cloneScopeView(snapshot.Eligible),
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
		ProcessingAt:         at,
		SchemaVersion:        DerivedSchemaVersionV1,
		OperationID:          operationID,
		Stage:                OperationStageScenarios,
		Scope:                boundary.Scope,
		ExpectedScopeVersion: snapshot.Structural.Version,
		Scenarios:            scenarios,
		Transitions:          transitions,
	}

	return e.commitBoundaryStage(ctx, commit)
}
