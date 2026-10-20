//go:build integration

package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func reopenPreservedOwner(t *testing.T, f *storefixture.Fixture) storefixture.Store {
	t.Helper()

	if err := f.Store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := f.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })

	return reopened
}

func TestPublicNonemptyMutationReplayAfterReopen(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		mutation := nativeMutation(t)

		want, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil {
			t.Fatal(err)
		}

		reopened := reopenPreservedOwner(t, f).(mutationStore)

		got, err := reopened.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("nonempty replay after reopen=%+v err=%v want=%+v", got, err, want)
		}

		assertMutationState(t, reopened, mutation.Scope, 1)
	})
}

func assertFailedMutationReopensEmpty(t *testing.T, f *storefixture.Fixture, scope memory.Scope) mutationStore {
	t.Helper()

	reopened := reopenPreservedOwner(t, f).(mutationStore)
	assertMutationState(t, reopened, scope, 0)

	changes, err := reopened.ScanScopeChanges(t.Context(), scope, 0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("failed mutation left changes after reopen=%+v err=%v", changes, err)
	}

	return reopened
}

func assertFailedBoundaryReopensUnreserved(t *testing.T, f *storefixture.Fixture,
	mutation memory.CanonicalMutation, empty bool,
) {
	t.Helper()

	reopened := reopenPreservedOwner(t, f)
	store := reopened.(mutationStore)
	assertMutationState(t, store, mutation.Scope, 0)

	_, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, mutation.Operation.OperationID)
	if err != nil || found {
		t.Fatalf("failed boundary persisted operation: found=%v err=%v", found, err)
	}

	verifier := reopened.(memory.CanonicalBoundaryIdentityStore)

	result, err := verifier.VerifyCanonicalBoundary(t.Context(), mutation.BoundaryIdentity.Boundary)
	if err != nil || result.Certified {
		t.Fatalf("failed boundary reserved certificate=%+v err=%v", result, err)
	}

	assertCorrectedBoundaryRetry(t, store, verifier, mutation, empty)
}

func assertCorrectedBoundaryRetry(t *testing.T, store mutationStore,
	verifier memory.CanonicalBoundaryIdentityStore, mutation memory.CanonicalMutation, empty bool,
) {
	t.Helper()

	boundary := mutation.BoundaryIdentity.Boundary
	boundary.Reason = memory.BoundaryReasonReset

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	mutation.BoundaryIdentity = &identity

	outcome, err := writeBoundary(t, store, mutation, empty)
	if err != nil || outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 {
		t.Fatalf("corrected boundary retry=%+v err=%v", outcome, err)
	}

	result, err := verifier.VerifyCanonicalBoundary(t.Context(), boundary)
	if err != nil || !result.Certified {
		t.Fatalf("corrected boundary certificate=%+v err=%v", result, err)
	}
}
