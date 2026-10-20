//go:build integration

package canonicalsql

import (
	"context"
	"reflect"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Aggregate sensitivity controls raw disclosure, not a standard revision's
// eligibility. The final snapshot must still validate its source and evidence.
func TestReaderBenignRecallWithSensitiveAggregate(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		h := prepareTraceHistory(t, owner)

		graph, err := owner.preflightTraceGraph(t.Context(), memory.TraceRequest{Scope: h.scope,
			Root: memory.RevisionRef{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID}, MaxNodes: 10})
		if err != nil {
			t.Fatal(err)
		}

		var captured readerSnapshot

		if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			rows, err := loadReaderRecords(ctx, tx, h.scope, []string{h.revision.RevisionID}, time.Now(), &captured)
			if err == nil && (len(rows) != 1 || rows[0].Text != "protected memory") {
				t.Fatalf("initial benign recall=%+v", rows)
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}

		makeReaderAggregateSensitive(t, owner, h)

		if err := owner.finalReaderBarrier(t.Context(), captured); err != nil {
			t.Fatalf("aggregate sensitivity denied unchanged benign revision: %v", err)
		}

		assertCode(t, owner.finalTracePrivacyBarrier(t.Context(), graph), memory.CodeForgotten)
		err = owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			return hydrateTraceGraph(ctx, tx, memory.TraceRequest{Scope: h.scope}, &graph)
		})
		assertCode(t, err, memory.CodeForgotten)
		assertBenignReaderAuthority(t, owner, h)

		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}

		assertBenignReaderAuthority(t, reopen(), h)
	})
}

func makeReaderAggregateSensitive(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		record, _, err := loadSourcePrivacy(ctx, tx, h.source)
		if err != nil {
			return err
		}

		record.State = sourcePrivacySensitive

		key, err := privacyKey(h.scope, SourcePrivacy, h.source.SourceID)
		if err != nil {
			return err
		}

		return putPrivacyRecord(ctx, tx, key, SourcePrivacy, record)
	}); err != nil {
		t.Fatal(err)
	}
}

func assertBenignReaderAuthority(t *testing.T, owner *Owner, h privacyHistory) {
	t.Helper()

	rows, err := owner.LoadRecallRecords(t.Context(), h.scope, []string{h.revision.RevisionID})
	if err != nil || len(rows) != 1 || rows[0].Text != "protected memory" {
		t.Fatalf("benign recall=%+v error=%v", rows, err)
	}

	active, err := owner.LoadActiveRecallRecords(t.Context(), h.scope, 10)
	if err != nil || len(active) != 1 || active[0].RevisionID != h.revision.RevisionID {
		t.Fatalf("active benign recall=%+v error=%v", active, err)
	}

	view, err := owner.LoadCanonicalBoundaryView(t.Context(), h.scope)
	if err != nil || len(view.LegacyToCanonical) != 1 {
		t.Fatalf("benign boundary hydration=%+v error=%v", view, err)
	}

	response, err := owner.Trace(t.Context(), memory.TraceRequest{Scope: h.scope,
		Root: memory.RevisionRef{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID}, MaxNodes: 10})
	assertCode(t, err, memory.CodeForgotten)

	if !reflect.DeepEqual(response, memory.TraceResponse{}) {
		t.Fatalf("denied raw trace retained content: %+v", response)
	}
}
