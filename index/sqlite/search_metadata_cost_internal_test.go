//go:build integration

package sqlite

import (
	"strings"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

//nolint:cyclop // Real staging, retrieval, call count and bounded streaming plans form one regression.
func TestSearchMetadataQueryCountDoesNotGrowPerChunk(t *testing.T) {
	for _, count := range []int{1, 16} {
		s, dir := openTestStore(t, true)
		g, coverage, initial := semanticFixture()

		chunks := make([]semantic.ProjectionChunk, count)
		for i := range chunks {
			chunks[i] = initial[0]
			chunks[i].Ordinal = uint32(i)
			chunks[i].SourceIDs = []string{"a", "z"}
			chunks[i].ConversationIDs = []string{"conversation"}
		}

		coverage[0].ChunkCount = uint32(count)

		seedSemanticSource(t, s, g)
		registerSemanticPrivacy(t, s, g, coverage, chunks)

		if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
			t.Fatal(err)
		}

		activateSemanticFixture(t, s, g)
		observer := observeReader(t, s, dir)

		snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
		if err != nil {
			t.Fatal(err)
		}

		observer.take()

		request := memory.RecallRequest{Scope: g.Scope, Query: "needle", Limit: 1, AsOf: &g.ReadyAt}
		hits, counts, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1)
		queries := observer.take()

		if err := snapshot.Close(); err != nil {
			t.Fatal(err)
		}

		if err != nil || len(hits) != 1 || hits[0].RevisionID != "revision" || counts.ScannedChunks != count {
			t.Fatalf("chunks=%d hits=%v counts=%v err=%v", count, hits, counts, err)
		}
		// Includes complete integrity, metadata, vector checks, eligibility and
		// ranking, independent of selected chunk count. No per-row SQL calls.
		if len(queries) > 12 {
			t.Errorf("%d chunks required %d SQL queries; want bounded <=12", count, len(queries))
		}

		for _, query := range queries {
			if strings.Contains(query.query, " m ON m.chunk_rowid=c.rowid") {
				assertMembershipStreamPlan(t, s, query)
			}
		}
	}
}

func assertMembershipStreamPlan(t *testing.T, store *Store, query observedQuery) {
	t.Helper()

	plan := observedQueryPlan(t, store, query)
	t.Log(plan)
	// A partial sort of one chunk's <=256 memberships is bounded. Sorting
	// the entire build would defeat the streaming memory contract.
	if strings.Contains(plan, "USE TEMP B-TREE FOR ORDER BY") {
		t.Error("membership stream sorts the whole build")
	}
}
