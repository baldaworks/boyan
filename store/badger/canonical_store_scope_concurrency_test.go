package badger

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStoreRejectsStaleCASAndAllowsIndependentScopes(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:cas", Kind: boyan.ScopeKindPersonal}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))); err != nil {
		t.Fatalf("first ApplyCanonicalMutation() error = %v", err)
	}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-2", canonicalRevision("revision-2", "item-2"))); err == nil {
		t.Fatal("stale CAS mutation succeeded")
	}
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, key := range []string{"canonical:independent-a", "canonical:independent-b"} {
		group.Add(1)
		go func(key string) {
			defer group.Done()
			scope := boyan.Scope{Key: key, Kind: boyan.ScopeKindPersonal}
			_, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision(key+"-revision", key+"-item")))
			errs <- err
		}(key)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("independent scope mutation error = %v", err)
		}
	}
}

func TestCanonicalStoreIsolatesEqualIDsAcrossScopes(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, scope := range []boyan.Scope{
		{Key: "canonical:scope-a", Kind: boyan.ScopeKindPersonal},
		{Key: "canonical:scope-b", Kind: boyan.ScopeKindPersonal},
	} {
		if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", canonicalRevision("revision-1", "item-1"))); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%q) error = %v", scope.Key, err)
		}
	}
}
