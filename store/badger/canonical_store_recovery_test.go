package badger //nolint:testpackage // Injects a failure through the private pre-commit hook.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
