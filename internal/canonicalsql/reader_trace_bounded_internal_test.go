//go:build integration

package canonicalsql

import (
	"context"
	"fmt"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"strconv"
	"testing"
	"time"
)

func TestTraceSourceLocalWork(t *testing.T) {
	counts := make(map[string][2]int)

	for _, history := range []int{0, 512} {
		t.Run(strconv.Itoa(history), func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				runTraceWork(t, owner, history, counts)
			})
		})
	}
}

func runTraceWork(t *testing.T, owner *Owner, history int, counts map[string][2]int) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	if _, err := owner.ApplyCanonicalMutation(t.Context(), historyMutation(h)); err != nil {
		t.Fatal(err)
	}

	seedReaderHistory(t, owner, h, history)
	request := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
		RevisionID: h.revision.RevisionID}, MaxNodes: 10}

	graph, err := owner.preflightTraceGraph(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}

	q := &readerWorkQuery{pageRows: make(map[Family]int)}

	if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		q.base, tx.q = tx.q, q

		return hydrateTraceGraph(ctx, tx, request, &graph)
	}); err != nil {
		t.Fatal(err)
	}

	backend := "sqlite"
	if owner.connection.pg != nil {
		backend = "postgres"
	}

	got := [2]int{q.reads, q.envelopes}
	t.Logf("backend=%s unrelated=%d reads=%d hydrated_envelopes=%d", backend, history, got[0], got[1])

	if history == 0 {
		counts[backend] = got
	} else if counts[backend] != got {
		t.Fatalf("history widened hydration: %v -> %v", counts[backend], got)
	}

	if err := owner.finalTracePrivacyBarrier(t.Context(), graph); err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryCollectionRealLimits(t *testing.T) {
	for _, count := range []int{0, 257, 2048, 2049} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) { runBoundaryCollectionLimit(t, owner, count) })
		})
	}
}

func runBoundaryCollectionLimit(t *testing.T, owner *Owner, count int) {
	t.Helper()

	if err := owner.PrepareSourcePrivacy(t.Context()); err != nil {
		t.Fatal(err)
	}

	h := newPrivacyHistory(t)
	seedBoundaryCollection(t, owner, h, count)

	var ids []string

	err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		var err error

		ids, err = collectBoundaryRevisionIDs(ctx, tx, h.scope, memory.MaxSnapshotItems)

		return err
	})
	if count > memory.MaxSnapshotItems {
		code, _, _ := memory.ClassifyError(err)
		if code != memory.CodeLimitExceeded || len(ids) != 0 {
			t.Fatalf("overflow=%d %v", len(ids), err)
		}

		return
	}

	if err != nil || len(ids) != count {
		t.Fatalf("collected=%d want=%d err=%v", len(ids), count, err)
	}
	// These heads have no source rows. Counting cannot be bypassed by omitted hydration.
	_, _, err = owner.loadBoundedBoundaryRecords(t.Context(), h.scope, 1, time.Now())
	if count > 1 {
		code, _, _ := memory.ClassifyError(err)
		if code != memory.CodeLimitExceeded {
			t.Fatalf("omitted hydration bypassed bound: %v", err)
		}
	}
}

func seedBoundaryCollection(t *testing.T, owner *Owner, h privacyHistory, count int) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		for i := range count {
			id := fmt.Sprintf("item-%05d", i)
			item, revision := h.item, h.revision

			item.ItemID, revision.ItemID, revision.RevisionID = id, id, "revision-"+id
			for family, value := range map[Family]any{Item: item, Revision: revision,
				Head: memory.ItemHead{ItemID: id, RevisionID: revision.RevisionID}} {
				keyID := id
				if family == Revision {
					keyID = revision.RevisionID
				}

				key, _ := privacyKey(h.scope, family, keyID)
				if err := putPrivacyRecord(ctx, tx, key, family, value); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
