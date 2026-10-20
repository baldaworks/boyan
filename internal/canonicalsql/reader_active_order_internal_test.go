//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	"reflect"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func seedActivePage(t *testing.T, owner *Owner, scope memory.Scope, ids []string) {
	t.Helper()

	h := newPrivacyHistory(t)
	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for _, id := range ids {
			item := h.item
			item.Scope, item.ItemID = scope, id
			revision := h.revision
			revision.ItemID, revision.RevisionID = id, id

			for _, record := range []struct {
				family Family
				value  any
			}{
				{Head, memory.ItemHead{ItemID: id, RevisionID: id}},
				{Item, item}, {Revision, revision},
			} {
				key := Key{Family: record.family, Scope: scope, Parts: [][]byte{[]byte(id)}}
				if err := tx.Put(ctx, key, envelope(t, record.family, record.value)); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestActivePageEncodedCursorAndExactScope(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := testScope()
		seedActivePage(t, owner, scope, []string{"aa", "z", "a\x00", "é", "aaa", "€"})
		seedActivePage(t, owner, memory.Scope{Key: scope.Key, Kind: memory.ScopeKindGroup}, []string{"a"})
		seedActivePage(t, owner, memory.Scope{Key: scope.Key + "other", Kind: scope.Kind}, []string{"b"})

		for _, tc := range []struct {
			name  string
			after string
			limit uint32
			want  []string
		}{
			{"length-before-bytes", "", 1, []string{"z"}},
			{"same-length-binary", "z", 2, []string{"a\x00", "aa"}},
			{"unicode-byte-length", "aa", 2, []string{"é", "aaa"}},
			{"absent-short-cursor", "x", 1, []string{"z"}},
			{"absent-binary-cursor", "a0", 2, []string{"aa", "é"}},
			{"terminal", "€", 1, []string{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				assertActivePageIDs(t, owner, scope, tc.after, tc.limit, tc.want)
			})
		}

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return tx.Delete(ctx, Key{Family: Head, Scope: scope, Parts: [][]byte{[]byte("z")}})
		}); err != nil {
			t.Fatal(err)
		}

		t.Run("deleted-cursor", func(t *testing.T) {
			assertActivePageIDs(t, owner, scope, "z", 1, []string{"a\x00"})
		})

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		heads, err := owner.ScanActiveHeads(ctx, memory.ActiveHeadScanRequest{Scope: scope, Limit: 1})
		if !errors.Is(err, context.Canceled) || len(heads) != 0 {
			t.Fatalf("canceled heads=%d err=%v", len(heads), err)
		}

		active, err := owner.ScanActiveMemory(ctx, memory.ActiveMemoryScanRequest{Scope: scope, Limit: 1})
		if !errors.Is(err, context.Canceled) || len(active) != 0 {
			t.Fatalf("canceled memory=%d err=%v", len(active), err)
		}
	})
}

func assertActivePageIDs(t *testing.T, owner *Owner, scope memory.Scope, after string, limit uint32, want []string) {
	t.Helper()

	heads, err := owner.ScanActiveHeads(t.Context(), memory.ActiveHeadScanRequest{Scope: scope,
		AfterItemID: after, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, 0, len(heads))
	for _, head := range heads {
		got = append(got, head.ItemID)
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("heads after %q = %q want %q", after, got, want)
	}

	active, err := owner.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope,
		AfterItemID: after, Limit: limit})
	if err != nil {
		t.Fatal(err)
	}

	got = got[:0]
	for _, record := range active {
		got = append(got, record.Item.ItemID)
		if record.RevisionID != record.Item.ItemID || len(record.Evidence) != 1 {
			t.Fatal("active item/revision/evidence binding lost")
		}
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("memory after %q = %q want %q", after, got, want)
	}
}

func TestActivePageBoundsHeadEnvelopeFetch(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := testScope()
		seedActivePage(t, owner, scope, []string{"aa", "z", "zz"})
		// An unselected malformed envelope must not be fetched or decoded.
		rawExec(t, owner.connection, "UPDATE "+owner.connection.table("head")+
			" SET envelope=$1 WHERE id=$2", []byte("corrupt"), []byte("zz"))

		if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			recorder := &hydrationQuery{base: tx.q}
			tx.q = recorder

			heads, err := scanReaderHeads(ctx, tx, memory.ActiveHeadScanRequest{Scope: scope, Limit: 1})
			if err != nil {
				return err
			}

			if len(heads) != 1 || heads[0].ItemID != "z" || recorder.hydrated != 1 {
				t.Fatalf("heads=%+v fetched envelopes=%d want z/1", heads, recorder.hydrated)
			}

			t.Logf("head_envelopes=%d head_limit=1", recorder.hydrated)

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestBoundaryCollectionAcceptsEncodedActiveOrder(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		scope := testScope()
		seedActivePage(t, owner, scope, []string{"aa", "z", "aaa"})

		err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			ids, err := collectBoundaryRevisionIDs(ctx, tx, scope, 3)
			if err != nil {
				return err
			}

			if !reflect.DeepEqual(ids, []string{"z", "aa", "aaa"}) {
				t.Fatalf("boundary revision IDs=%q want z,aa,aaa", ids)
			}

			return nil
		})
		if err != nil {
			t.Fatalf("valid encoded head progress rejected: %v", err)
		}
	})
}
