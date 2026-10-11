package badger_test

import (
	"context"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreBatchesSourceProvenance(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:source-batch", Kind: boyan.ScopeKindPersonal}
	for index, revisionID := range []string{"revision-1", "revision-2"} {
		mutation := canonicalRevisionMutation(
			scope,
			uint64(index),
			"operation-"+revisionID,
			canonicalRevision(revisionID, "item-"+revisionID),
		)
		if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%s) error = %v", revisionID, err)
		}
	}

	first, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", "", 1)
	if err != nil || len(first) != 1 || cursor != first[0] {
		t.Fatalf("first SourceRevisionBatch() = %#v, %q, error = %v", first, cursor, err)
	}

	assertCanonicalSourceBatchExhaustion(t, store, scope, first, cursor)
}

func assertCanonicalSourceBatchExhaustion(
	t *testing.T,
	store *canonicalbadger.CanonicalStore,
	scope boyan.Scope,
	first []string,
	cursor string,
) {
	t.Helper()

	second, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(second) != 1 || second[0] == first[0] || cursor != second[0] {
		t.Fatalf("second SourceRevisionBatch() = %#v, %q, error = %v", second, cursor, err)
	}

	last, cursor, err := store.SourceRevisionBatch(context.Background(), scope, "source-1", cursor, 1)
	if err != nil || len(last) != 0 || cursor != "" {
		t.Fatalf("final SourceRevisionBatch() = %#v, %q, error = %v", last, cursor, err)
	}
}
