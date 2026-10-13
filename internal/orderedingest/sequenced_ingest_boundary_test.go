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

	proof := &boundaryFixtureProof{}
	runtime, err := newSequencedIngestRuntimeWithParts(
		sequences,
		boundaryFixtureCanonical{proof: proof, fakeCanonicalStore: fakeCanonicalStore{
			state: boyan.ScopeState{
				SchemaVersion: boyan.CanonicalSchemaVersionV1,
				Scope:         scope,
				Version:       2,
				ChangeSeq:     3,
			},
		}},
		nil,
		boyan.DerivationRef{},
		nil,
		boundaryFixtureProcessor{proof: proof, fakeBoundaryProcessor: fakeBoundaryProcessor{outcome: boyan.BoundaryOutcome{
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
		}}},
		nil,
		SequencedIngestConfig{},
	)
	requireNoError(t, err, "newSequencedIngestRuntimeWithParts() error = %v")

	return runtime
}

type boundaryFixtureProof struct {
	identity *boyan.CanonicalBoundaryIdentityCommit
}

type boundaryFixtureCanonical struct {
	fakeCanonicalStore

	proof *boundaryFixtureProof
}

func (s boundaryFixtureCanonical) VerifyCanonicalBoundary(_ context.Context, input boyan.Boundary,
) (boyan.BoundaryIdentityResult, error) {
	expected, err := boyan.NewCanonicalBoundaryIdentityCommit(input)
	if err != nil {
		return boyan.BoundaryIdentityResult{}, err
	}

	if s.proof.identity == nil {
		return boyan.BoundaryIdentityResult{SafelyNew: true}, nil
	}

	if s.proof.identity.Certificate != expected.Certificate {
		return boyan.BoundaryIdentityResult{}, boyan.PermanentError(boyan.CodeConflict, "changed boundary fixture", nil)
	}

	return boyan.BoundaryIdentityResult{Certified: true}, nil
}

type boundaryFixtureProcessor struct {
	fakeBoundaryProcessor

	proof *boundaryFixtureProof
}

func (p boundaryFixtureProcessor) ProcessBoundary(
	ctx context.Context, input boyan.Boundary,
) (boyan.BoundaryOutcome, error) {
	outcome, err := p.fakeBoundaryProcessor.ProcessBoundary(ctx, input)
	if err != nil {
		return outcome, err
	}

	identity, err := boyan.NewCanonicalBoundaryIdentityCommit(input)
	if err != nil {
		return boyan.BoundaryOutcome{}, err
	}

	p.proof.identity = &identity

	return outcome, nil
}
