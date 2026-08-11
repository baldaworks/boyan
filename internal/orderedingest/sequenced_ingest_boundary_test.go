package orderedingest

import (
	"context"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestSequencedIngestRuntimeAcceptsBoundaryAndPersistsReplay(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:sequenced-boundary", Kind: boyan.ScopeKindPersonal}
	sequences := newMemoryIngestSequenceStore()
	runtime, err := newSequencedIngestRuntimeWithParts(
		sequences,
		fakeCanonicalStore{state: boyan.ScopeState{SchemaVersion: boyan.CanonicalSchemaVersionV1, Scope: scope, Version: 2, ChangeSeq: 3}},
		nil,
		boyan.DerivationRef{},
		nil,
		fakeBoundaryProcessor{outcome: boyan.BoundaryOutcome{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			Scope:         scope,
			Scenarios:     boyan.OperationOutcome{SchemaVersion: boyan.DerivedSchemaVersionV1, OperationID: "scenario-op", Stage: boyan.OperationStageScenarios, Scope: scope, ScopeVersion: 1},
			Profile:       boyan.OperationOutcome{SchemaVersion: boyan.DerivedSchemaVersionV1, OperationID: "profile-op", Stage: boyan.OperationStageProfile, Scope: scope, ScopeVersion: 2},
		}},
		nil,
		SequencedIngestConfig{},
	)
	if err != nil {
		t.Fatalf("newSequencedIngestRuntimeWithParts() error = %v", err)
	}
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })

	request := testSequencedBoundary(t, 1, "boundary-fingerprint")
	first, err := runtime.ApplySequencedBoundary(context.Background(), request)
	if err != nil {
		t.Fatalf("ApplySequencedBoundary(first) error = %v", err)
	}
	if first.Disposition != boyan.IngestDispositionAccepted || first.ScopeVersion != 2 || first.ScopeChangeSeq != 3 || len(first.OperationIDs) != 2 {
		t.Fatalf("first boundary outcome = %+v, want accepted replayable canonical state", first)
	}

	second, err := runtime.ApplySequencedBoundary(context.Background(), request)
	if err != nil {
		t.Fatalf("ApplySequencedBoundary(replay) error = %v", err)
	}
	if second.Disposition != boyan.IngestDispositionDuplicate {
		t.Fatalf("duplicate boundary outcome = %+v, want duplicate replay", second)
	}
}
