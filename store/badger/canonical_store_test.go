package badger //nolint:testpackage // Tests private record codec and error translation.

import (
	"errors"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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

func TestCanonicalStoreErrorMapsTxnTooBig(t *testing.T) {
	err := canonicalStoreError("test", badger.ErrTxnTooBig)

	var memoryErr *boyan.Error
	if !errors.As(err, &memoryErr) || memoryErr.Code != boyan.CodeLimitExceeded {
		t.Fatalf("canonicalStoreError() = %#v", err)
	}
}
