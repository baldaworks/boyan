//go:build integration

package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type emptyTemporalSynthesis struct{}

func (emptyTemporalSynthesis) SynthesizeScenarios(context.Context,
	m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	return nil, nil
}
func (emptyTemporalSynthesis) SynthesizeProfile(context.Context,
	m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	return nil, nil
}

func TestEmptyBoundaryStagesPersistCapturedProcessingTime(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		store, reader := newModelRetryStore(t, factory)
		turn := modelRetryTurn(t)
		at := turn.CompletedAt.Add(24 * time.Hour)
		adapter, err := ops.NewCanonicalBoundaryStore(store, reader)
		requireNoError(t, err, "adapter: %v")

		model := emptyTemporalSynthesis{}
		processor, err := boundary.NewBoundaryProcessorWithClock(adapter, model, model, m.LegacyDerivationRef(),
			func() time.Time { return at })
		requireNoError(t, err, "processor: %v")
		export, err := m.NewBoundary(turn.Scope, turn.Conversation, "close", m.BoundaryReasonClose, turn.CompletedAt)
		requireNoError(t, err, "boundary: %v")
		outcome, err := processor.ProcessBoundary(context.Background(), export)
		requireNoError(t, err, "empty boundary: %v")

		for _, stage := range []m.OperationOutcome{outcome.Scenarios, outcome.Profile} {
			operation, found, err := store.LoadCanonicalOperation(context.Background(), turn.Scope, stage.OperationID)
			requireNoError(t, err, "operation: %v")

			if !found {
				t.Errorf("stage operation missing: found=%v record=%+v", found, operation)
			}
		}

		changes, err := store.ScanScopeChanges(context.Background(), turn.Scope, 0, 10)
		requireNoError(t, err, "stage changes: %v")

		if len(changes) != 2 {
			t.Fatalf("stage changes=%+v", changes)
		}

		for _, change := range changes {
			if !change.OccurredAt.Equal(at) {
				t.Errorf("empty stage time=%v want %v", change.OccurredAt, at)
			}
		}

		_, err = adapter.Commit(context.Background(), m.CommitRequest{
			SchemaVersion: m.DerivedSchemaVersionV1, OperationID: "missing-processing-time",
			Stage: m.OperationStageScenarios, Scope: turn.Scope, ExpectedScopeVersion: outcome.Profile.ScopeVersion})

		code, _, classified := m.ClassifyError(err)
		if !classified || code != m.CodeInvalidDerived {
			t.Errorf("zero processing time error=%v", err)
		}
	})
}
