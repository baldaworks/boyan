package canonicalcore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	forget "github.com/baldaworks/boyan/internal/forgetops"
	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryProofAndInheritedBoundsSurviveReopen(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, forgotten := range []bool{false, true} {
			t.Run(map[bool]string{false: "live", true: "forgotten"}[forgotten], func(t *testing.T) {
				t.Parallel()

				var path string

				f := newInheritanceFixture(t, false, func(t *testing.T, directory string) (coreOwner, coreReader) {
					path = directory

					return openCoreAuthority(t, directory, factory)
				})
				expected, err := f.processor.ProcessBoundary(context.Background(), f.export)
				requireNoError(t, err, "synthesize: %v")
				revisions := reopenStageRevisions(t, f.store, f.export.Scope, expected)

				if forgotten {
					forgetReopenScope(t, f)
				}

				reopened := reopenBoundaryStore(t, f.store, path, factory)
				assertReopenProofAndReplay(t, f, reopened, expected)
				assertReopenCapabilityGuard(t, reopened, f.export)

				actual := reopenStageRevisions(t, reopened, f.export.Scope, expected)
				if !reflect.DeepEqual(revisions, actual) {
					t.Fatal("reopen changed inherited temporal/trust metadata")
				}

				assertReopenInheritedSearch(t, reopened, f, forgotten)
			})
		}
	})
}

func reopenBoundaryStore(t *testing.T, source coreOwner, path string, factory coreFactory) coreOwner {
	t.Helper()
	requireNoError(t, source.Close(), "close before reopen: %v")
	reopened, _ := openCoreAuthority(t, path, factory)
	t.Cleanup(func() { requireNoError(t, reopened.Close(), "close reopened: %v") })

	return reopened
}

func reopenStageRevisions(t *testing.T, store coreOwner, scope m.Scope,
	outcome m.BoundaryOutcome) []m.MemoryRevision {
	t.Helper()

	var result []m.MemoryRevision

	for _, stage := range []m.OperationOutcome{outcome.Scenarios, outcome.Profile} {
		record, found, err := store.LoadCanonicalOperation(context.Background(), scope, stage.OperationID)
		requireNoError(t, err, "operation: %v")

		if !found {
			t.Fatal("stage operation lost")
		}

		revisions, err := store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
			Scope: scope, RevisionIDs: record.Outcome.RevisionIDs})
		requireNoError(t, err, "revisions: %v")

		if len(revisions) != 1 || revisions[0].GroundingBoundsVersion != 1 || len(revisions[0].GroundingParents) == 0 {
			t.Fatal("synthesized bounded trust metadata missing")
		}

		result = append(result, revisions...)
	}

	return result
}

func forgetReopenScope(t *testing.T, f *inheritanceFixture) {
	t.Helper()

	adapter, err := forget.NewCanonicalForgetAdapter(f.store)
	requireNoError(t, err, "forget adapter: %v")
	_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
		SchemaVersion: m.DerivedSchemaVersionV1, Scope: f.export.Scope, RequestID: "reopen-forget", ForgottenAt: f.now})
	requireNoError(t, err, "forget: %v")
}

func assertReopenProofAndReplay(t *testing.T, f *inheritanceFixture, reopened coreOwner,
	expected m.BoundaryOutcome) {
	t.Helper()

	ctx := context.Background()
	identity, err := reopened.VerifyCanonicalBoundary(ctx, f.export)
	requireNoError(t, err, "reopened proof: %v")

	if !identity.Certified {
		t.Fatal("reopen lost immutable certificate")
	}

	changed := f.export
	changed.Reason = m.BoundaryReasonReset
	_, err = reopened.VerifyCanonicalBoundary(ctx, changed)
	requirePermanentIdentityConflict(t, err)
	reader := readerForCoreOwner(t, reopened)
	adapter, err := ops.NewCanonicalBoundaryStore(reopened, reader)
	requireNoError(t, err, "adapter: %v")
	processor, err := boundary.NewBoundaryProcessorWithClock(adapter, f.model, f.model, testProcessorDerivation(),
		func() time.Time { return f.now })
	requireNoError(t, err, "processor: %v")
	before, err := reopened.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "state before: %v")

	scenarios, profiles := f.model.scenarioCalls, f.model.profileCalls
	actual, err := processor.ProcessBoundary(ctx, f.export)
	requireNoError(t, err, "reopened replay: %v")
	after, err := reopened.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "state after: %v")

	if !reflect.DeepEqual(expected, actual) || before != after ||
		scenarios != f.model.scenarioCalls || profiles != f.model.profileCalls {
		t.Fatal("reopen replay changed outcomes/state/models")
	}
}

func assertReopenInheritedSearch(t *testing.T, store coreOwner, f *inheritanceFixture, forgotten bool) {
	t.Helper()

	reader := readerForCoreOwner(t, store)
	service, err := read.NewRecallService(reader, nil)
	requireNoError(t, err, "recall: %v")

	for _, at := range []time.Time{f.now, f.expiry} {
		response, err := service.Search(context.Background(), m.RecallRequest{
			Scope: f.export.Scope, Query: "shared", Limit: 10, AsOf: &at,
		})
		requireNoError(t, err, "search: %v")

		want := 2
		if forgotten || !at.Before(f.expiry) {
			want = 0
		}

		if len(response.Results) != want {
			t.Fatalf("reopened search at%v=%d want%d", at, len(response.Results), want)
		}
	}
}

func TestBoundaryProofFreeLegacyReopenStaysUnproven(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		t.Parallel()

		var path string

		_, _, turn, _, source := nativeFlow(t, func(t *testing.T, directory string) (coreOwner, coreReader) {
			path = directory

			return openCoreAuthority(t, directory, factory)
		})
		at := time.Now().UTC()
		seedLegacyAtomOnly(t, source, turn.Scope, at)
		destination := reopenBoundaryStore(t, source, path, factory)
		export, err := m.NewBoundary(turn.Scope, turn.Conversation, "unproven-reopen", m.BoundaryReasonClose, at)
		requireNoError(t, err, "boundary: %v")
		_, err = destination.VerifyCanonicalBoundary(context.Background(), export)
		requirePermanentIdentityConflict(t, err)

		model := &legacyClassificationModel{}
		reader := readerForCoreOwner(t, destination)
		adapter, err := ops.NewCanonicalBoundaryStore(destination, reader)
		requireNoError(t, err, "adapter: %v")
		processor, err := boundary.NewBoundaryProcessor(adapter, model, model, testProcessorDerivation())
		requireNoError(t, err, "processor: %v")
		_, err = processor.ProcessBoundary(context.Background(), export)
		requirePermanentIdentityConflict(t, err)

		if model.calls != 0 {
			t.Fatal("reopen manufactured certification for old derived history")
		}
	})
}

func assertReopenCapabilityGuard(t *testing.T, store coreOwner, export m.Boundary) {
	t.Helper()

	ctx := context.Background()
	next, err := m.NewBoundary(export.Scope, export.Conversation, "new-after-reopen", export.Reason, export.OccurredAt)
	requireNoError(t, err, "next: %v")
	result, err := store.VerifyCanonicalBoundary(ctx, next)
	requireNoError(t, err, "reopened capability: %v")

	if !result.SafelyNew || result.Certified {
		t.Fatal("reopened marker cannot authorize fresh boundary")
	}

	operation, err := m.ProcessingOperationID(m.OperationStageAtoms, "unproven-after-reopen", m.LegacyDerivationRef())
	requireNoError(t, err, "operation: %v")
	state, err := store.LoadScopeState(ctx, export.Scope)
	requireNoError(t, err, "state: %v")
	_, err = store.CommitCanonicalOperation(ctx, m.CanonicalOperationCommitRequest{Scope: export.Scope,
		ExpectedScopeVersion: state.Version, OperationID: operation, Fingerprint: "unproven-fingerprint",
		CommittedAt: export.OccurredAt})
	requirePermanentIdentityConflict(t, err)
}
