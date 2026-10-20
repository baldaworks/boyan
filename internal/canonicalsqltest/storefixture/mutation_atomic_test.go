package storefixture_test

import (
	"context"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"sync"
	"testing"
)

func TestPublicMutationAtomicFailures(t *testing.T) {
	for _, table := range []string{"source", "revision", "boundary_certificate", "operation"} {
		t.Run(table, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				mutation := nativeMutation(t)
				mutation.BoundaryIdentity = mutationBoundary(t, mutation.Scope)
				before := probe.stamps(t)
				release := probe.failInserts(t, table)
				_, err := store.ApplyCanonicalMutation(t.Context(), mutation)

				release()

				if err == nil {
					t.Fatal("injected row/proof/precommit failure committed")
				}

				assertMutationState(t, store, mutation.Scope, 0)

				if !reflect.DeepEqual(before, probe.stamps(t)) {
					t.Fatal("failed mutation changed clock/stamps")
				}

				store = assertFailedMutationReopensEmpty(t, f, mutation.Scope)

				outcome, err := store.ApplyCanonicalMutation(t.Context(), mutation)
				if err != nil || outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 {
					t.Fatalf("retry after reopen=%+v err=%v", outcome, err)
				}

				assertMutationState(t, store, mutation.Scope, 1)
			})
		})
	}
}

func TestSourcePrivacyAtomicLateSensitiveAndMonotone(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		later := laterMutation(mutation, "late-sensitive", 1)
		later.Revisions[0].Sensitivity = memory.SensitivitySensitive
		before := probe.stamps(t)
		release := probe.failInserts(t, "operation")
		_, err := store.ApplyCanonicalMutation(t.Context(), later)

		release()

		if err == nil {
			t.Fatal("late failure ignored")
		}

		if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "standard" ||
			!reflect.DeepEqual(before, probe.stamps(t)) {
			t.Fatal("aborted sensitivity escaped transaction")
		}

		if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
			t.Fatal(err)
		}

		later = laterMutation(mutation, "late-standard", 2)
		if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
			t.Fatal(err)
		}

		if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "sensitive" {
			t.Fatal("later standard cleared sensitivity")
		}
	})
}

func laterMutation(initial memory.CanonicalMutation, id string, version uint64) memory.CanonicalMutation {
	mutation := initial
	mutation.Operation.OperationID = id
	mutation.Operation.Fingerprint = id
	mutation.ExpectedScopeVersion = version
	mutation.Sources = nil
	mutation.Messages = nil
	mutation.Payloads = nil

	mutation.Revisions = append([]memory.MemoryRevision(nil), initial.Revisions...)
	mutation.Revisions[0].RevisionID = id
	mutation.Heads = []memory.ItemHead{{ItemID: mutation.Revisions[0].ItemID, RevisionID: id}}

	return mutation
}

func TestPublicMutationCASAndCancellation(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		before := probe.stamps(t)
		if _, err := store.ApplyCanonicalMutation(ctx, mutation); err != context.Canceled {
			t.Fatalf("cancel=%v", err)
		}

		if !reflect.DeepEqual(before, probe.stamps(t)) {
			t.Fatal("cancel wrote")
		}

		var group sync.WaitGroup

		results := make(chan error, 2)

		for _, id := range []string{"first", "second"} {
			group.Go(func() {
				m := mutation
				m.Operation.OperationID = id

				_, err := store.ApplyCanonicalMutation(t.Context(), m)
				results <- err
			})
		}

		group.Wait()
		close(results)

		wins := 0

		for err := range results {
			if err == nil {
				wins++
			} else {
				assertMutationCode(t, err, memory.CodeConflict)
			}
		}

		if wins != 1 {
			t.Fatalf("CAS winners=%d", wins)
		}

		assertMutationState(t, store, mutation.Scope, 1)

		changes, err := store.ScanScopeChanges(t.Context(), mutation.Scope, 0, 10)
		if err != nil || len(changes) != 1 {
			t.Fatalf("changes=%+v %v", changes, err)
		}
	})
}

func TestPublicMutationImmutableConflict(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		mutation.ExpectedScopeVersion = 1
		mutation.Operation.OperationID = "changed"
		mutation.Items[0].MemoryKey = "changed"
		before := probe.stamps(t)
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		assertMutationCode(t, err, memory.CodeConflict)

		if !reflect.DeepEqual(before, probe.stamps(t)) {
			t.Fatal("immutable failure modified rows")
		}

		var head memory.ItemHead
		probe.read(t, mutation.Scope, canonicalsql.Head, "item", &head)

		if head.RevisionID != "revision" {
			t.Fatal("head changed")
		}
	})
}
