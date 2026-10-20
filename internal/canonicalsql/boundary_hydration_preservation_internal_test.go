//go:build integration

package canonicalsql

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryCollectorNativeHydrationBindings(t *testing.T) {
	for _, kind := range []string{"revision", "item", "duplicate-alias"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				h := prepareTraceHistory(t, owner)
				assertSingleBoundaryRevision(t, owner, h.scope, h.revision.RevisionID)
				corruptBoundaryHydrationBinding(t, owner, h, kind)

				got, err := owner.LoadCanonicalBoundaryView(t.Context(), h.scope)
				assertCode(t, err, memory.CodeStoreFailure)

				if len(got.LegacyToCanonical) != 0 || len(got.Snapshot.Atoms) != 0 {
					t.Fatal("corrupt boundary returned partial model input")
				}
			})
		})
	}
}

func corruptBoundaryHydrationBinding(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	if kind == "duplicate-alias" {
		item := h.item
		item.ItemID = "alias"

		err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if err := tx.Put(ctx, Key{Family: Item, Scope: h.scope, Parts: [][]byte{[]byte(item.ItemID)}},
				envelope(t, Item, item)); err != nil {
				return err
			}

			return tx.Put(ctx, Key{Family: Head, Scope: h.scope, Parts: [][]byte{[]byte(item.ItemID)}},
				envelope(t, Head, memory.ItemHead{ItemID: item.ItemID, RevisionID: h.revision.RevisionID}))
		})
		if err != nil {
			t.Fatal(err)
		}

		return
	}

	revision := h.revision
	if kind == "revision" {
		revision.RevisionID = "unrequested-revision"
	} else {
		revision.ItemID = "unrequested-item"
	}

	// Keep a checksummed full DTO; only the requested identity binding changes.
	rawExec(t, owner.connection, "UPDATE "+owner.connection.table("revision")+
		" SET envelope=$1 WHERE id=$2", envelope(t, Revision, revision), []byte(h.revision.RevisionID))
}

func TestBoundaryCollectorExcludesSupersededRevision(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := prepareTraceHistory(t, owner)
		assertSingleBoundaryRevision(t, owner, h.scope, h.revision.RevisionID)

		next := historyMutation(h)
		next.ExpectedScopeVersion = 1
		next.Operation.OperationID = "replacement"
		next.Operation.Fingerprint = "replacement"
		next.Revisions[0].RevisionID = "replacement"
		next.Revisions[0].Revision = 2

		next.Heads[0].RevisionID = "replacement"
		if _, err := owner.ApplyCanonicalMutation(t.Context(), next); err != nil {
			t.Fatal(err)
		}

		// SQL hydrates each validated current head once in the same snapshot;
		// it does not accept a separately supplied inactive/duplicate DTO list.
		assertSingleBoundaryRevision(t, owner, h.scope, "replacement")
	})
}

func assertSingleBoundaryRevision(t *testing.T, owner *Owner, scope memory.Scope, revisionID string) {
	t.Helper()

	records, err := owner.LoadActiveRecallRecords(t.Context(), scope, 10)
	if err != nil || len(records) != 1 || records[0].RevisionID != revisionID ||
		records[0].State != memory.RevisionStateActive {
		t.Fatalf("active boundary hydration=%+v want=%q err=%v", records, revisionID, err)
	}

	view, err := owner.LoadCanonicalBoundaryView(t.Context(), scope)
	if err != nil || len(view.LegacyToCanonical) != 1 {
		t.Fatalf("boundary hydration=%+v err=%v", view, err)
	}

	for _, ref := range view.LegacyToCanonical {
		if ref.RevisionID != revisionID {
			t.Fatalf("boundary retained unrequested/superseded revision=%+v", ref)
		}
	}
}
