package badger_test

import (
	"context"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreRestoresReplayAndScopeState(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")

	store := openCanonicalTestStore(t, directory)

	scope := boyan.Scope{Key: "canonical:restart", Kind: boyan.ScopeKindPersonal}
	mutation := canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))

	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatalf("ApplyCanonicalMutation() error = %v", err)
	}

	closeCanonicalTestStore(t, store)

	store, err = canonicalbadger.OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || replayed.ScopeVersion != first.ScopeVersion ||
		replayed.ChangeSeq != first.ChangeSeq {
		t.Fatalf("replay after reopen = %#v, error = %v", replayed, err)
	}

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.Version != first.ScopeVersion ||
		state.ChangeSeq != first.ChangeSeq {
		t.Fatalf("state after reopen = %#v, error = %v", state, err)
	}
}
