package badger_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreDeniesSourceByExactScope(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")

	store := openCanonicalTestStore(t, directory)

	scope := boyan.Scope{Key: "canonical:deny-a", Kind: boyan.ScopeKindPersonal}
	if err := store.DenySource(
		context.Background(),
		scope,
		"source-1",
		time.Date(2026, time.August, 5, 12, 0, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("DenySource() error = %v", err)
	}

	if err := store.DenySource(
		context.Background(),
		scope,
		"source-1",
		time.Date(2026, time.August, 5, 12, 1, 0, 0, time.UTC),
	); err != nil {
		t.Fatalf("DenySource() retry error = %v", err)
	}

	denied, err := store.IsSourceDenied(context.Background(), scope, "source-1")
	if err != nil || !denied {
		t.Fatalf("IsSourceDenied() = %t, error = %v", denied, err)
	}

	otherScope := boyan.Scope{Key: "canonical:deny-b", Kind: boyan.ScopeKindPersonal}
	if denied, err := store.IsSourceDenied(context.Background(), otherScope, "source-1"); err != nil || denied {
		t.Fatalf("other scope IsSourceDenied() = %t, error = %v", denied, err)
	}

	closeCanonicalTestStore(t, store)

	store, err = canonicalbadger.OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}

	t.Cleanup(func() { _ = store.Close() })

	if denied, err := store.IsSourceDenied(context.Background(), scope, "source-1"); err != nil || !denied {
		t.Fatalf("reopened IsSourceDenied() = %t, error = %v", denied, err)
	}
}
