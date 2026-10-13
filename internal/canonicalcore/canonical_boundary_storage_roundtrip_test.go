package canonicalcore_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	forget "github.com/baldaworks/boyan/internal/forgetops"
	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func TestBoundaryProofAndInheritedBoundsStorageRoundTrip(t *testing.T) {
	for _, logical := range []bool{false, true} {
		for _, forgotten := range []bool{false, true} {
			name := map[bool]string{false: "native", true: "logical"}[logical] + "/" +
				map[bool]string{false: "live", true: "forgotten"}[forgotten]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				f := newInheritanceFixture(t, false)
				expected, err := f.processor.ProcessBoundary(context.Background(), f.export)
				requireNoError(t, err, "synthesize: %v")

				revisions := roundTripStageRevisions(t, f.store, f.export.Scope, expected)
				if forgotten {
					forgetRoundTripScope(t, f)
				}

				restored := restoreBoundaryStore(t, f.store, logical)
				assertRoundTripProofAndReplay(t, f, restored, expected)
				assertRoundTripCapabilityGuard(t, restored, f.export)

				actual := roundTripStageRevisions(t, restored, f.export.Scope, expected)
				if !reflect.DeepEqual(revisions, actual) {
					t.Fatal("roundtrip changed inherited temporal/trust metadata")
				}

				assertRoundTripInheritedSearch(t, restored, f, forgotten)
			})
		}
	}
}

func restoreBoundaryStore(t *testing.T, source *b.CanonicalStore, logical bool) *b.CanonicalStore {
	t.Helper()

	ctx := context.Background()

	var encoded bytes.Buffer
	if logical {
		requireNoError(t, source.ExportCanonicalLogical(ctx, &encoded), "logical export: %v")
	} else {
		_, err := source.WriteCanonicalBackup(ctx, &encoded, 0)
		requireNoError(t, err, "native backup: %v")
	}

	destination, err := b.Open(t.TempDir())
	requireNoError(t, err, "destination: %v")
	t.Cleanup(func() { requireNoError(t, destination.Close(), "close destination: %v") })

	if logical {
		err = destination.ImportCanonicalLogical(ctx, bytes.NewReader(encoded.Bytes()))
	} else {
		err = destination.RestoreCanonicalBackup(ctx, bytes.NewReader(encoded.Bytes()), 0)
	}

	requireNoError(t, err, "restore: %v")
	requireNoError(t, destination.VerifyCanonicalIntegrity(ctx), "restored integrity: %v")

	return destination
}

func roundTripStageRevisions(t *testing.T, store *b.CanonicalStore, scope m.Scope,
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

func forgetRoundTripScope(t *testing.T, f *inheritanceFixture) {
	t.Helper()

	adapter, err := forget.NewCanonicalForgetAdapter(f.store)
	requireNoError(t, err, "forget adapter: %v")
	_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
		SchemaVersion: m.DerivedSchemaVersionV1, Scope: f.export.Scope, RequestID: "roundtrip-forget", ForgottenAt: f.now})
	requireNoError(t, err, "forget: %v")
}

func assertRoundTripProofAndReplay(t *testing.T, f *inheritanceFixture, restored *b.CanonicalStore,
	expected m.BoundaryOutcome) {
	t.Helper()

	ctx := context.Background()
	identity, err := restored.VerifyCanonicalBoundary(ctx, f.export)
	requireNoError(t, err, "restored proof: %v")

	if !identity.Certified {
		t.Fatal("restore lost immutable certificate")
	}

	changed := f.export
	changed.Reason = m.BoundaryReasonReset
	_, err = restored.VerifyCanonicalBoundary(ctx, changed)
	requirePermanentIdentityConflict(t, err)
	reader, err := b.NewCanonicalReader(restored)
	requireNoError(t, err, "reader: %v")
	adapter, err := ops.NewCanonicalBoundaryStore(restored, reader)
	requireNoError(t, err, "adapter: %v")
	processor, err := boundary.NewBoundaryProcessorWithClock(adapter, f.model, f.model, testProcessorDerivation(),
		func() time.Time { return f.now })
	requireNoError(t, err, "processor: %v")
	before, err := restored.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "state before: %v")

	scenarios, profiles := f.model.scenarioCalls, f.model.profileCalls
	actual, err := processor.ProcessBoundary(ctx, f.export)
	requireNoError(t, err, "restored replay: %v")
	after, err := restored.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "state after: %v")

	if !reflect.DeepEqual(expected, actual) || before != after ||
		scenarios != f.model.scenarioCalls || profiles != f.model.profileCalls {
		t.Fatal("restore replay changed outcomes/state/models")
	}
}

func assertRoundTripInheritedSearch(t *testing.T, store *b.CanonicalStore, f *inheritanceFixture, forgotten bool) {
	t.Helper()

	reader, err := b.NewCanonicalReader(store)
	requireNoError(t, err, "reader: %v")
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
			t.Fatalf("restored search at%v=%d want%d", at, len(response.Results), want)
		}
	}
}

func TestBoundaryProofFreeLegacyStorageRoundTripStaysUnproven(t *testing.T) {
	for _, logical := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "logical"}[logical], func(t *testing.T) {
			t.Parallel()
			_, _, turn, _, source := nativeFlow(t)
			at := time.Now().UTC()
			seedLegacyAtomOnly(t, source, turn.Scope, at)
			destination := restoreBoundaryStore(t, source, logical)
			export, err := m.NewBoundary(turn.Scope, turn.Conversation, "unproven-import", m.BoundaryReasonClose, at)
			requireNoError(t, err, "boundary: %v")
			_, err = destination.VerifyCanonicalBoundary(context.Background(), export)
			requirePermanentIdentityConflict(t, err)

			model := &legacyClassificationModel{}
			reader, err := b.NewCanonicalReader(destination)
			requireNoError(t, err, "reader: %v")
			adapter, err := ops.NewCanonicalBoundaryStore(destination, reader)
			requireNoError(t, err, "adapter: %v")
			processor, err := boundary.NewBoundaryProcessor(adapter, model, model, testProcessorDerivation())
			requireNoError(t, err, "processor: %v")
			_, err = processor.ProcessBoundary(context.Background(), export)
			requirePermanentIdentityConflict(t, err)

			if model.calls != 0 {
				t.Fatal("import manufactured certification for old derived history")
			}
		})
	}
}

func assertRoundTripCapabilityGuard(t *testing.T, store *b.CanonicalStore, export m.Boundary) {
	t.Helper()

	ctx := context.Background()
	next, err := m.NewBoundary(export.Scope, export.Conversation, "new-after-restore", export.Reason, export.OccurredAt)
	requireNoError(t, err, "next: %v")
	result, err := store.VerifyCanonicalBoundary(ctx, next)
	requireNoError(t, err, "restored capability: %v")

	if !result.SafelyNew || result.Certified {
		t.Fatal("restored marker cannot authorize fresh boundary")
	}

	operation, err := m.ProcessingOperationID(m.OperationStageAtoms, "unproven-after-restore", m.LegacyDerivationRef())
	requireNoError(t, err, "operation: %v")
	state, err := store.LoadScopeState(ctx, export.Scope)
	requireNoError(t, err, "state: %v")
	_, err = store.CommitCanonicalOperation(ctx, m.CanonicalOperationCommitRequest{Scope: export.Scope,
		ExpectedScopeVersion: state.Version, OperationID: operation, Fingerprint: "unproven-fingerprint",
		CommittedAt: export.OccurredAt})
	requirePermanentIdentityConflict(t, err)
}
