package sqlitegate_test

import (
	"reflect"
	"sort"
	"testing"
	"time"

	bleveindex "github.com/baldaworks/boyan/index/bleve"
	"github.com/baldaworks/boyan/pkg/memory"
)

// This comparison harness belongs to the pre-migration baseline, not the new
// adapter. Remove its retired-engine import with TASK-016; retain the shared
// corpus/expectations and captured results for TASK-019's old checkout.
func openLegacyBaseline(t *testing.T) (*bleveindex.BleveRecallProjection, memory.Scope, time.Time) {
	t.Helper()

	projection, err := bleveindex.Open(t.TempDir(), bleveindex.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = projection.Close() })

	generation, err := projection.NewGeneration("baseline-v1")
	if err != nil {
		t.Fatal(err)
	}

	at := time.Date(2026, time.August, 11, 9, 0, 0, 0, time.UTC)
	scope := memory.Scope{Key: "baseline:v1", Kind: memory.ScopeKindGroup}

	for index, doc := range lexicalControls() {
		category := memory.AtomCategoryDecision
		if err := generation.Index(t.Context(), memory.RecallProjectionDocument{
			Scope: scope, ItemID: doc.id, RevisionID: doc.id, Revision: 1,
			Kind: memory.MemoryKindState, Category: &category, MemoryKey: memory.MemoryKey(doc.id),
			Text: doc.text, CreatedAt: at, Temporal: memory.Temporal{ObservedAt: at},
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard,
			SourceIDs: []string{"baseline-source"}, ConversationIDs: []string{"baseline-conversation"},
			ScopeChangeSeq: uint64(index + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := generation.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := generation.Close(); err != nil {
		t.Fatal(err)
	}

	if err := projection.ActivateGenerationForScope(t.Context(), scope, "baseline-v1"); err != nil {
		t.Fatal(err)
	}

	return projection, scope, at
}

func TestLegacyBleveRelevanceBaseline(t *testing.T) {
	t.Parallel()

	projection, scope, at := openLegacyBaseline(t)

	for _, control := range lexicalRelevanceQueries() {
		t.Run(control.query, func(t *testing.T) {
			var previous []memory.RecallProjectionHit

			for attempt := range 3 {
				hits, err := projection.SearchRecall(t.Context(), memory.RecallRequest{
					Scope: scope, Query: control.query, Limit: 10, AsOf: &at,
				})
				if err != nil {
					t.Fatal(err)
				}

				ids := baselineIDs(t, control.query, hits, attempt == 0)

				if attempt > 0 && !reflect.DeepEqual(hits, previous) {
					t.Logf("old-engine repeated order/scores changed: %+v then %+v", previous, hits)
				}

				previous = hits

				want := control.want
				if control.query == "café" {
					// Documented normalization improvement, not old-engine parity.
					want = nil
				}

				// Old Bleve does not promise the new binary ID tie order.
				sort.Strings(ids)

				if !reflect.DeepEqual(ids, want) {
					t.Fatalf("query=%q IDs=%v want=%v", control.query, ids, want)
				}
			}
		})
	}
}

func baselineIDs(t *testing.T, query string, hits []memory.RecallProjectionHit, logRanks bool) []string {
	t.Helper()

	var ids []string

	for rank, hit := range hits {
		ids = append(ids, hit.RevisionID)
		if logRanks {
			t.Logf("query=%q rank=%d revision=%s score=%.17g", query, rank+1, hit.RevisionID, hit.Score)
		}
	}

	if logRanks && len(hits) == 0 {
		t.Logf("query=%q hits=0", query)
	}

	return ids
}
