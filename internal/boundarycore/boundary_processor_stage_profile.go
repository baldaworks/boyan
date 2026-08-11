package boundarycore

import (
	"context"

	. "github.com/baldaworks/boyan/pkg/memory"
)

func (e *BoundaryProcessor) processProfileStage(ctx context.Context, boundary Boundary) (OperationOutcome, error) {
	operationID, snapshot, prior, found, err := e.prepareBoundaryStage(ctx, boundary, OperationStageProfile)
	if err != nil || found {
		return prior, err
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
