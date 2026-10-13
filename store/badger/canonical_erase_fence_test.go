package badger //nolint:testpackage // Tests private transactional ordering and corruption.

import (
	"context"
	"errors"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestScopeEraseFenceRejectsWriterAdmittedBeforeBoundary(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:interleave", Kind: memory.ScopeKindPersonal}
	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
	store.beforeCanonicalMutationCommit = func() error {
		_, err := store.BeginScopeErase(context.Background(), request)

		return err
	}

	_, err = store.CommitCanonicalOperation(context.Background(), memory.CanonicalOperationCommitRequest{
		Scope:       scope,
		OperationID: "boundary",
		Fingerprint: "fp",
		CommittedAt: time.Now(),
	})
	if !memory.IsRetryable(err) {
		t.Fatalf("admitted writer committed across fence: %v", err)
	}

	store.beforeCanonicalMutationCommit = nil

	journal, found, err := store.LoadScopeErase(context.Background(), scope, "erase")
	if err != nil || !found || journal.BaselineVersion != 0 {
		t.Fatalf("journal=%+v found=%t err=%v", journal, found, err)
	}
}

func TestScopeEraseCorruptionFailsClosed(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:corrupt", Kind: memory.ScopeKindPersonal}

	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
	if _, err := store.BeginScopeErase(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	key, err := canonicalKey(scope, badgerRecordEraseJournal, "erase")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Set(key, []byte("corrupt")) }); err != nil {
		t.Fatal(err)
	}

	_, _, err = store.LoadScopeErase(context.Background(), scope, "erase")
	requireEraseStoreFailure(t, err)
	err = store.DenySource(context.Background(), scope, "source", time.Now())
	requireEraseStoreFailure(t, err)

	if err := store.CompleteScopeErase(context.Background(), scope, "erase"); err == nil {
		t.Fatal("corrupt journal completed")
	}
}

func TestScopeEraseRawPayloadGuardAndOwnedProgress(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:owner", Kind: memory.ScopeKindPersonal}

	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
	if _, err := store.BeginScopeErase(context.Background(), request); err != nil {
		t.Fatal(err)
	}

	err = store.db.Update(func(txn *badger.Txn) error {
		return checkScopeEraseFence(txn, scope, "erase")
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.DeletePayload(context.Background(), memory.PayloadRef{ID: "payload", Digest: "digest", ByteSize: 1})
	if !memory.IsRetryable(err) {
		t.Fatalf("raw payload delete=%v", err)
	}

	err = store.db.Update(func(txn *badger.Txn) error {
		return checkScopeEraseFence(txn, scope, "foreign")
	})
	if !memory.IsRetryable(err) {
		t.Fatalf("foreign owner=%v", err)
	}
}

func requireEraseStoreFailure(t *testing.T, err error) {
	t.Helper()

	var typed *memory.Error
	if !errors.As(err, &typed) || typed.Code != memory.CodeStoreFailure {
		t.Fatalf("expected store failure, got %v", err)
	}
}
