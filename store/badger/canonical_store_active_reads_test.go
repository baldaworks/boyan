package badger_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
	canonicalbadger "github.com/baldaworks/boyan/store/badger"
)

func TestCanonicalStoreProvidesBoundedRevisionAndHeadReads(t *testing.T) {
	store := openCanonicalTestStore(t, filepath.Join(t.TempDir(), "memory.badger"))

	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "canonical:projection-read", Kind: boyan.ScopeKindPersonal}

	for index := 1; index <= 2; index++ {
		id := fmt.Sprintf("%d", index)
		if _, err := store.ApplyCanonicalMutation(
			context.Background(),
			canonicalRevisionMutation(
				scope,
				uint64(index-1),
				"operation-"+id,
				canonicalRevision("revision-"+id, "item-"+id),
			),
		); err != nil {
			t.Fatalf("ApplyCanonicalMutation(%d) error = %v", index, err)
		}
	}

	revisions, err := store.LoadCanonicalRevisions(
		context.Background(),
		boyan.CanonicalRevisionReadRequest{Scope: scope, RevisionIDs: []string{"revision-2", stateTestRevisionOneID}},
	)
	if err != nil || len(revisions) != 2 || revisions[0].RevisionID != "revision-2" ||
		revisions[1].RevisionID != stateTestRevisionOneID {
		t.Fatalf("LoadCanonicalRevisions() = %#v, error %v", revisions, err)
	}

	assertCanonicalActivePages(t, store, scope)
}

func assertCanonicalActivePages(t *testing.T, store *canonicalbadger.CanonicalStore, scope boyan.Scope) {
	t.Helper()

	heads, err := store.ScanActiveHeads(context.Background(), boyan.ActiveHeadScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(heads) != 1 || heads[0].ItemID != "item-1" {
		t.Fatalf("ScanActiveHeads(first) = %#v, error %v", heads, err)
	}

	heads, err = store.ScanActiveHeads(
		context.Background(),
		boyan.ActiveHeadScanRequest{Scope: scope, AfterItemID: heads[0].ItemID, Limit: 1},
	)
	if err != nil || len(heads) != 1 || heads[0].ItemID != "item-2" {
		t.Fatalf("ScanActiveHeads(next) = %#v, error %v", heads, err)
	}

	assertCanonicalActiveEvidence(t, store, scope)
}

func assertCanonicalActiveEvidence(t *testing.T, store *canonicalbadger.CanonicalStore, scope boyan.Scope) {
	t.Helper()

	active, err := store.ScanActiveMemory(context.Background(), boyan.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
	if err != nil || len(active) != 1 || active[0].Item.ItemID != "item-1" ||
		len(active[0].Evidence) != 1 {
		t.Fatalf("ScanActiveMemory() = %#v, error %v", active, err)
	}
}

func TestCanonicalActivePagesKeepEncodedOrder(t *testing.T) {
	t.Parallel()
	store := openCanonicalTestStore(t, t.TempDir())
	t.Cleanup(func() { _ = store.Close() })

	scope := boyan.Scope{Key: "test:cursor", Kind: boyan.ScopeKindPersonal}
	for i, id := range []string{"z", "aa", "ab", "aaa"} {
		_,
			err := store.ApplyCanonicalMutation(context.Background(),
			canonicalRevisionMutation(scope,
				uint64(i),
				"operation-"+id,
				canonicalRevision("revision-"+id,
					id)))
		if err != nil {
			t.Fatalf("commit: %v", err)
		}
	}

	cursor := ""
	for _, want := range []string{"z", "aa", "ab", "aaa"} {
		heads,
			err := store.ScanActiveHeads(context.Background(),
			boyan.ActiveHeadScanRequest{Scope: scope,
				AfterItemID: cursor,
				Limit:       1})
		if err != nil {
			t.Fatalf("scan: %v", err)
		}

		if len(heads) != 1 || heads[0].ItemID != want {
			t.Fatalf("after %q: %+v, want %s", cursor, heads, want)
		}

		cursor = want
	}

	heads,
		err := store.ScanActiveHeads(context.Background(),
		boyan.ActiveHeadScanRequest{Scope: scope,
			AfterItemID: "a0",
			Limit:       1})
	if err != nil {
		t.Fatalf("missing cursor: %v", err)
	}

	if len(heads) != 1 || heads[0].ItemID != "aa" {
		t.Fatalf("missing cursor skipped next encoded key: %+v", heads)
	}
}
