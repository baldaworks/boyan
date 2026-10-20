//go:build integration

package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	forget "github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type legacyClassificationModel struct{ calls int }

func (model *legacyClassificationModel) SynthesizeScenarios(context.Context,
	m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	model.calls++

	return nil, nil
}

func (model *legacyClassificationModel) SynthesizeProfile(context.Context,
	m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	model.calls++

	return nil, nil
}

func TestBoundaryLegacyClassificationBeforeModels(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, legacy := range []bool{false, true} {
			t.Run(map[bool]string{false: "native only", true: "legacy atom only"}[legacy], func(t *testing.T) {
				t.Parallel()
				reader, _, turn, _, store := nativeFlow(t, factory)
				ctx := context.Background()
				at := time.Now().UTC()
				export, err := m.NewBoundary(turn.Scope, turn.Conversation, "classify", m.BoundaryReasonClose, at)
				requireNoError(t, err, "boundary: %v")

				if legacy {
					seedLegacyAtomOnly(t, store, turn.Scope, at)
				}

				model := &legacyClassificationModel{}
				adapter, err := ops.NewCanonicalBoundaryStore(store, reader)
				requireNoError(t, err, "adapter: %v")
				processor, err := boundary.NewBoundaryProcessorWithClock(adapter, model, model,
					testProcessorDerivation(), func() time.Time { return at })
				requireNoError(t, err, "processor: %v")
				before, err := store.LoadScopeState(ctx, turn.Scope)
				requireNoError(t, err, "before: %v")

				_, err = processor.ProcessBoundary(ctx, export)
				if !legacy {
					assertNativeBoundaryClassified(t, store, model, export, err)

					return
				}

				requirePermanentIdentityConflict(t, err)
				after, err := store.LoadScopeState(ctx, turn.Scope)
				requireNoError(t, err, "after: %v")

				if model.calls != 0 || before != after {
					t.Fatal("legacy scope entered models or changed state")
				}

				forgotten, err := forget.NewCanonicalForgetAdapter(store)
				requireNoError(t, err, "forget adapter: %v")
				_, err = forgotten.ForgetScope(ctx, m.ForgetScopeCommand{
					SchemaVersion: m.DerivedSchemaVersionV1, Scope: turn.Scope,
					RequestID: "forget-unproven", ForgottenAt: at.Add(time.Hour),
				})
				requireNoError(t, err, "forget: %v")
				_, err = processor.ProcessBoundary(ctx, export)
				requirePermanentIdentityConflict(t, err)

				if model.calls != 0 {
					t.Fatal("forget admitted unproven history to models")
				}
			})
		}
	})
}

func seedLegacyAtomOnly(t *testing.T, store coreOwner, scope m.Scope, at time.Time) {
	t.Helper()

	ctx := context.Background()
	state, stateErr := store.LoadScopeState(ctx, scope)
	requireNoError(t, stateErr, "state: %v")

	operation, operationErr := m.ProcessingOperationID(m.OperationStageAtoms, "old-export", m.LegacyDerivationRef())
	requireNoError(t, operationErr, "legacy operation: %v")

	_, err := store.CommitCanonicalOperation(ctx, m.CanonicalOperationCommitRequest{
		Scope: scope, ExpectedScopeVersion: state.Version, OperationID: operation,
		Fingerprint: "old-operation-fingerprint", CommittedAt: at,
	})
	requireNoError(t, err, "seed old atom operation: %v")
}

func assertNativeBoundaryClassified(
	t *testing.T, store coreOwner, model *legacyClassificationModel, export m.Boundary, err error,
) {
	t.Helper()
	requireNoError(t, err, "native-only scope: %v")

	if model.calls != 2 {
		t.Fatalf("model calls=%d", model.calls)
	}

	identity, identityErr := store.VerifyCanonicalBoundary(context.Background(), export)
	requireNoError(t, identityErr, "identity: %v")

	if !identity.Certified {
		t.Fatal("successful native-only scope lacks certificate")
	}
}
