package orderedingest //nolint:testpackage // Exercises newSequencedIngestRuntimeWithParts within the package.

import (
	"context"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestSequencedIngestRuntimeAcceptsBoundaryAndPersistsReplay(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:sequenced-boundary", Kind: boyan.ScopeKindPersonal}
	sequences := newMemoryIngestSequenceStore()

	runtime := newBoundaryReplayTestRuntime(t, scope, sequences)

	requireNoError(t, runtime.Start(context.Background()), "Start() error = %v")

	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	request := testSequencedBoundary(t, 1, "boundary-fingerprint")

	first, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "ApplySequencedBoundary(first) error = %v")

	if first.Disposition != boyan.IngestDispositionAccepted || first.ScopeVersion != 2 ||
		first.ScopeChangeSeq != 3 ||
		len(first.OperationIDs) != 2 {
		t.Fatalf("first boundary outcome = %+v, want accepted replayable canonical state", first)
	}

	second, err := runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "ApplySequencedBoundary(replay) error = %v")

	if second.Disposition != boyan.IngestDispositionDuplicate {
		t.Fatalf("duplicate boundary outcome = %+v, want duplicate replay", second)
	}
}

func newBoundaryReplayTestRuntime(
	t *testing.T,
	scope boyan.Scope,
	sequences *memoryIngestSequenceStore,
) *SequencedIngestRuntime {
	t.Helper()

	runtime, err := newSequencedIngestRuntimeWithParts(
		sequences,
		fakeCanonicalStore{
			state: boyan.ScopeState{
				SchemaVersion: boyan.CanonicalSchemaVersionV1,
				Scope:         scope,
				Version:       2,
				ChangeSeq:     3,
			},
		},
		nil,
		boyan.DerivationRef{},
		nil,
		fakeBoundaryProcessor{outcome: boyan.BoundaryOutcome{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			Scope:         scope,
			Scenarios: boyan.OperationOutcome{
				SchemaVersion: boyan.DerivedSchemaVersionV1,
				OperationID:   "scenario-op",
				Stage:         boyan.OperationStageScenarios,
				Scope:         scope,
				ScopeVersion:  1,
			},
			Profile: boyan.OperationOutcome{
				SchemaVersion: boyan.DerivedSchemaVersionV1,
				OperationID:   "profile-op",
				Stage:         boyan.OperationStageProfile,
				Scope:         scope,
				ScopeVersion:  2,
			},
		}},
		nil,
		SequencedIngestConfig{},
	)
	requireNoError(t, err, "newSequencedIngestRuntimeWithParts() error = %v")

	return runtime
}
