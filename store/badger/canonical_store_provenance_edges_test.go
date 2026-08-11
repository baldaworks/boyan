package badger

import (
	"context"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestCanonicalStorePersistsBidirectionalProvenance(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:provenance", Kind: boyan.ScopeKindPersonal}
	first := canonicalRevision("revision-1", "item-1")
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 0, "operation-1", first)); err != nil {
		t.Fatalf("first ApplyCanonicalMutation() error = %v", err)
	}
	second := canonicalRevision("revision-2", "item-2")
	second.Parents = []string{first.RevisionID}
	if _, err := store.ApplyCanonicalMutation(context.Background(), canonicalRevisionMutation(scope, 1, "operation-2", second)); err != nil {
		t.Fatalf("second ApplyCanonicalMutation() error = %v", err)
	}
	forwardKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceForward, second.RevisionID, first.RevisionID)
	if err != nil {
		t.Fatalf("badgerProvenanceKey() error = %v", err)
	}
	reverseKey, err := badgerProvenanceKey(scope, badgerRecordProvenanceReverse, first.RevisionID, second.RevisionID)
	if err != nil {
		t.Fatalf("badgerProvenanceKey() error = %v", err)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		var forward, reverse badgerProvenanceEdge
		if err := getCanonicalRecord(txn, forwardKey, badgerRecordProvenanceForward, &forward); err != nil {
			return err
		}
		if err := getCanonicalRecord(txn, reverseKey, badgerRecordProvenanceReverse, &reverse); err != nil {
			return err
		}
		if forward != reverse || forward.ChildRevisionID != second.RevisionID || forward.ParentRevisionID != first.RevisionID {
			t.Fatalf("provenance edges = %#v / %#v", forward, reverse)
		}
		return nil
	}); err != nil {
		t.Fatalf("read provenance edges: %v", err)
	}
	sourceKey, err := badgerProvenanceKey(scope, badgerRecordSourceRevision, "source-1", first.RevisionID)
	if err != nil {
		t.Fatalf("source provenance key error = %v", err)
	}
	if err := store.db.View(func(txn *badger.Txn) error {
		var edge badgerProvenanceEdge
		return getCanonicalRecord(txn, sourceKey, badgerRecordSourceRevision, &edge)
	}); err != nil {
		t.Fatalf("read source provenance edge: %v", err)
	}
}

func TestCanonicalStoreBatchesSourceProvenance(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := boyan.Scope{Key: "canonical:source-batch", Kind: boyan.ScopeKindPersonal}
	for index, revisionID := range []string{"revision-1", "revision-2"} {
		mutation := canonicalRevisionMutation(scope, uint64(index), "operation-"+revisionID, canonicalRevision(revisionID, "item-"+revisionID))
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%s) error = %v", revisionID, err)
		}
	}
	first, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", "", 1)
	if err != nil || len(first) != 1 || cursor != first[0] {
		t.Fatalf("first SourceRevisionBatch() = %#v, %q, error = %v", first, cursor, err)
	}
	second, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(second) != 1 || second[0] == first[0] || cursor != second[0] {
		t.Fatalf("second SourceRevisionBatch() = %#v, %q, error = %v", second, cursor, err)
	}
	last, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(last) != 0 || cursor != "" {
		t.Fatalf("final SourceRevisionBatch() = %#v, %q, error = %v", last, cursor, err)
	}
}
