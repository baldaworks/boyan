package badger_test

import (
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/boyantest"
	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestOpenPortableAPIReopensCanonicalDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "portable.badger")

	store, err := canonicalbadger.Open(
		directory,
		canonicalbadger.Config{SyncWrites: true, DetectConflicts: true, NumVersionsToKeep: 1},
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	closeCanonicalTestStore(t, store)

	reopened, err := canonicalbadger.Open(directory)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}

	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalStoreCanonicalContract(t *testing.T) {
	boyantest.RunCanonicalStoreContract(t, func(t *testing.T) boyan.CanonicalStore {
		store, err := canonicalbadger.OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
		if err != nil {
			t.Fatalf("OpenCanonicalStore() error = %v", err)
		}

		t.Cleanup(func() { _ = store.Close() })

		return store
	})
}

func TestCanonicalStoreOwnsOneWritableDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")

	first := openCanonicalTestStore(t, directory)

	if _, err := canonicalbadger.OpenCanonicalStore(directory); err == nil {
		t.Fatal("second writable Badger store opened")
	}

	closeCanonicalTestStore(t, first)

	reopened := openCanonicalTestStore(t, directory)

	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalStoreRequiresDirectory(t *testing.T) {
	if _, err := canonicalbadger.OpenCanonicalStore(" "); err == nil {
		t.Fatal("empty directory opened")
	}
}

func TestCanonicalStoreValueLogGCValidation(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	if err := store.RunValueLogGC(0); err == nil {
		t.Fatal("RunValueLogGC(0) succeeded")
	}

	if err := store.RunValueLogGC(0.5); err != nil {
		t.Fatalf("RunValueLogGC() error = %v", err)
	}
}
