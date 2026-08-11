package badger

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/boyantest"
	boyan "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestOpenPortableAPIReopensCanonicalDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "portable.badger")
	store, err := Open(directory, Config{SyncWrites: true, DetectConflicts: true, NumVersionsToKeep: 1})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := Open(directory)
	if err != nil {
		t.Fatalf("Open(reopen) error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalStoreCanonicalContract(t *testing.T) {
	boyantest.RunCanonicalStoreContract(t, func(t *testing.T) boyan.CanonicalStore {
		store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
		if err != nil {
			t.Fatalf("OpenCanonicalStore() error = %v", err)
		}
		t.Cleanup(func() { _ = store.Close() })
		return store
	})
}

func TestCanonicalStoreOwnsOneWritableDirectory(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "memory.badger")
	first, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	if _, err := OpenCanonicalStore(directory); err == nil {
		t.Fatal("second writable Badger store opened")
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	reopened, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
}

func TestCanonicalRecordCodec(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key := []byte("record")
	if err := store.db.Update(func(txn *badger.Txn) error {
		return putCanonicalRecord(txn, key, "scope", map[string]string{"key": "value"})
	}); err != nil {
		t.Fatalf("write record error = %v", err)
	}
	var got map[string]string
	if err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, "scope", &got)
	}); err != nil {
		t.Fatalf("read record error = %v", err)
	}
	if got["key"] != "value" {
		t.Fatalf("record = %#v", got)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, "operation", &got)
	}); err == nil {
		t.Fatal("wrong record type decoded")
	}
}

func TestCanonicalStoreRequiresDirectory(t *testing.T) {
	if _, err := OpenCanonicalStore(" "); err == nil {
		t.Fatal("empty directory opened")
	}
}

func TestCanonicalStoreValueLogGCValidation(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.RunValueLogGC(0); err == nil {
		t.Fatal("RunValueLogGC(0) succeeded")
	}
	if err := store.RunValueLogGC(0.5); err != nil {
		t.Fatalf("RunValueLogGC() error = %v", err)
	}
}

func TestCanonicalStoreErrorMapsTxnTooBig(t *testing.T) {
	err := canonicalStoreError("test", badger.ErrTxnTooBig)
	var memoryErr *boyan.Error
	if !errors.As(err, &memoryErr) || memoryErr.Code != boyan.CodeLimitExceeded {
		t.Fatalf("canonicalStoreError() = %#v", err)
	}
}
