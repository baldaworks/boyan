package badger //nolint:testpackage // Injects a failure through the private pre-commit hook.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStoreFaultBeforeCommitLeavesNoPartialMutationAfterReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")

	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}

	scope := boyan.Scope{Key: "canonical:fault", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-fault", canonicalRevision("revision-fault", "item-fault"))

	store.beforeCanonicalMutationCommit = func() error { return errors.New("injected pre-commit fault") }
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("ApplyCanonicalMutation() error = nil, want injected pre-commit failure")
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	assertCanonicalRetryAfterFault(t, store, scope, mutation)
}

func assertCanonicalRetryAfterFault(
	t *testing.T,
	store *CanonicalStore,
	scope boyan.Scope,
	mutation boyan.CanonicalMutation,
) {
	t.Helper()

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("LoadScopeState(after failed commit) = %#v, error %v", state, err)
	}

	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 10)
	if err != nil || len(changes) != 0 {
		t.Fatalf("ScanScopeChanges(after failed commit) = %#v, error %v", changes, err)
	}

	outcome, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 {
		t.Fatalf("ApplyCanonicalMutation(retry) = %#v, error %v", outcome, err)
	}
}

func TestBoundaryCertificateFaultRollsBackBothWriteRoutes(t *testing.T) {
	for _, nonempty := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "nonempty"}[nonempty], func(t *testing.T) {
			directory := t.TempDir()

			store, err := OpenCanonicalStore(directory)
			if err != nil {
				t.Fatal(err)
			}

			scope := boyan.Scope{Key: "test:boundary-certificate-fault", Kind: boyan.ScopeKindPersonal}

			boundary, err := boyan.NewBoundary(scope, boyan.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
				"close", boyan.BoundaryReasonClose, time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC))
			if err != nil {
				t.Fatal(err)
			}

			identity, err := boyan.NewCanonicalBoundaryIdentityCommit(boundary)
			if err != nil {
				t.Fatal(err)
			}

			store.beforeCanonicalMutationCommit = func() error { return errors.New("injected boundary commit failure") }

			_, err = commitBoundaryFaultFixture(store, identity, nonempty)
			if err == nil {
				t.Fatal("injected commit failure ignored")
			}

			if err := store.Close(); err != nil {
				t.Fatal(err)
			}

			store, err = OpenCanonicalStore(directory)
			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})

			assertBoundaryFaultRollback(t, store, identity)
			assertBoundaryFaultRetry(t, store, identity, nonempty)
		})
	}
}

func commitBoundaryFaultFixture(store *CanonicalStore, identity boyan.CanonicalBoundaryIdentityCommit,
	nonempty bool) (boyan.CanonicalMutationOutcome, error) {
	if nonempty {
		mutation := canonicalRevisionMutation(
			identity.Boundary.Scope, 0, "boundary-fault", canonicalRevision("revision-fault", "item-fault"),
		)
		mutation.BoundaryIdentity = &identity

		return store.ApplyCanonicalMutation(context.Background(), mutation)
	}

	return store.CommitCanonicalOperation(context.Background(), boyan.CanonicalOperationCommitRequest{
		Scope: identity.Boundary.Scope, OperationID: "boundary-fault", Fingerprint: "boundary-fault-fingerprint",
		CommittedAt: identity.Boundary.OccurredAt, BoundaryIdentity: &identity})
}

func assertBoundaryFaultRollback(t *testing.T, store *CanonicalStore, identity boyan.CanonicalBoundaryIdentityCommit) {
	t.Helper()

	boundary := identity.Boundary
	scope := boundary.Scope

	result, err := store.VerifyCanonicalBoundary(context.Background(), boundary)
	if err != nil || result.Certified {
		t.Fatalf("failed transaction reserved boundary: %+v %v", result, err)
	}

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != 0 || state.ChangeSeq != 0 {
		t.Fatalf("failed transaction advanced state: %+v %v", state, err)
	}

	_, found, err := store.LoadCanonicalOperation(context.Background(), scope, "boundary-fault")
	if err != nil || found {
		t.Fatalf("failed transaction persisted operation: found=%v err=%v", found, err)
	}
}

func assertBoundaryFaultRetry(
	t *testing.T, store *CanonicalStore, identity boyan.CanonicalBoundaryIdentityCommit, nonempty bool,
) {
	t.Helper()

	boundary := identity.Boundary

	boundary.Reason = boyan.BoundaryReasonReset

	corrected, err := boyan.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	outcome, err := commitBoundaryFaultFixture(store, corrected, nonempty)
	if err != nil || outcome.ScopeVersion != 1 {
		t.Fatalf("corrected retry after failed commit: %+v %v", outcome, err)
	}

	result, err := store.VerifyCanonicalBoundary(context.Background(), boundary)
	if err != nil || !result.Certified {
		t.Fatalf("successful retry lacks certificate: %+v %v", result, err)
	}
}
