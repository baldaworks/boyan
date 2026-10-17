//nolint:lll // Keep hand-authored SQL fixtures and literal filter expectations readable.
package sqlite

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

// This catches separate transactions, missing SQL ranking and silent corruption filtering.
//
//nolint:cyclop // The MVCC assertions exercise every resource and corruption outcome.
func TestSearchSnapshotRanksAndRejectsCorruptSelectedBuild(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	if got := snapshot.LexicalManifest(); got.GenerationID != g.LexicalGenerationID {
		t.Fatalf("manifest=%#v", got)
	}

	state, found, err := snapshot.SemanticState()
	if err != nil || !found || state.BuildID != g.BuildID {
		t.Fatalf("state=%#v found=%v err=%v", state, found, err)
	}

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 10, AsOf: &g.ReadyAt}

	hits, counts, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{0.6, 0.8}}, 10)
	if err != nil || len(hits) != 1 || counts.ScannedChunks != 1 || math.Abs(hits[0].Score-0.6) > 1e-6 {
		t.Fatalf("hits=%#v counts=%#v err=%v", hits, counts, err)
	}

	execTest(t, s.writer, "UPDATE semantic_chunk SET embedding=?", []byte{0, 0, 0, 64, 0, 0, 0, 0})
	// The already-open view retains its immutable, valid generation.
	if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 10); err != nil {
		t.Fatal(err)
	}

	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	bad, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer bad.Close()

	if _, _, err = bad.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 10); !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("corruption=%v", err)
	}
}

func stageSearchFixture(t *testing.T, s *Store, g semantic.Generation, chunks []semantic.ProjectionChunk) {
	t.Helper()

	byID := map[string]semantic.CoverageRecord{}
	for _, chunk := range chunks {
		record := byID[chunk.RevisionID]
		record.ItemID = chunk.ItemID
		record.RevisionID = chunk.RevisionID
		record.Revision = chunk.Revision
		record.ContentHash = chunk.ContentHash
		record.ChunkCount++
		byID[chunk.RevisionID] = record
	}

	var coverage []semantic.CoverageRecord
	for _, record := range byID {
		coverage = append(coverage, record)
	}

	registerSemanticPrivacy(t, s, g, coverage, chunks)

	if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
}

// A chunk limit, missing window or dot-product-only implementation disagrees
// with this independently accumulated true cosine oracle (float32 tolerance 1e-6).
//
//nolint:cyclop,funlen // Real ranking and its execution plan have independent failure gates.
func TestSearchSnapshotExactRevisionOracleAndPlan(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, base := semanticFixture()
	seedSemanticSource(t, s, g)

	var chunks []semantic.ProjectionChunk

	for i := 0; i < 2500; i++ {
		chunk := base[0]
		chunk.Ordinal = uint32(i)
		chunk.RevisionID = "dense"
		chunk.ItemID = "dense-item"
		chunks = append(chunks, chunk)
	}

	for i, vector := range [][]float32{{0, 1}, {-1, 0}, {0, -1}, {0.6, 0.8}, {0.6001, 0.8}} {
		chunk := base[0]
		chunk.RevisionID = fmt.Sprintf("other-%d", i)
		chunk.ItemID = chunk.RevisionID
		chunk.Vector = vector
		chunks = append(chunks, chunk)
	}

	stageSearchFixture(t, s, g, chunks)

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 100, AsOf: &g.ReadyAt}
	for _, queries := range [][][]float32{{{1, 0}}, {{1, 0}, {0, 1}, {-1, 0}, {0, -1}}} {
		got, counts, err := snapshot.SearchSemantic(t.Context(), request, queries, 100)
		if err != nil {
			t.Fatal(err)
		}

		want := searchOracle(chunks, queries)
		if counts.ScannedChunks != len(chunks) || len(got) != len(want) {
			t.Fatalf("count=%#v got=%#v want=%#v", counts, got, want)
		}

		for i := range got {
			if got[i].RevisionID != want[i].RevisionID || got[i].ChunkOrdinal != want[i].ChunkOrdinal || math.Abs(got[i].Score-want[i].Score) > 1e-6 {
				t.Fatalf("at %d got=%#v want=%#v", i, got[i], want[i])
			}
		}
	}

	args := []any{encodeQueryVector([]float32{1, 0})}
	args = append(args, semanticEligibleArgs(snapshot.(*sqlSearchSnapshot).state, request, g.ReadyAt)...)
	args = append(args, 2)

	rows, err := snapshot.(*sqlSearchSnapshot).tx.QueryContext(t.Context(), "EXPLAIN QUERY PLAN "+semanticRankQuery(1), args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	materialized, selected := false, false

	for rows.Next() {
		var (
			id, parent, unused int
			detail             string
		)
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}

		t.Log(detail)
		materialized = materialized || strings.Contains(detail, "MATERIALIZE eligible")
		selected = selected || (strings.Contains(detail, "SEARCH c USING") && strings.Contains(detail, "build_id=?"))
	}

	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}

	if !materialized || !selected {
		t.Fatalf("materialized=%v selected-build=%v", materialized, selected)
	}
}

func searchOracle(chunks []semantic.ProjectionChunk, queries [][]float32) []semantic.DenseMatch {
	winners := map[string]semantic.DenseMatch{}

	for _, chunk := range chunks {
		best := -1.0

		for _, query := range queries {
			var dot, left, right float64
			for i, value := range query {
				dot += float64(value) * float64(chunk.Vector[i])
				left += float64(value) * float64(value)
				right += float64(chunk.Vector[i]) * float64(chunk.Vector[i])
			}

			best = max(best, max(-1, min(1, dot/math.Sqrt(left*right))))
		}

		prior, found := winners[chunk.RevisionID]
		if !found || best > prior.Score || best == prior.Score && int(chunk.Ordinal) < prior.ChunkOrdinal {
			winners[chunk.RevisionID] = semantic.DenseMatch{RevisionID: chunk.RevisionID, ChunkOrdinal: int(chunk.Ordinal), Score: best}
		}
	}

	var result []semantic.DenseMatch
	for _, winner := range winners {
		result = append(result, winner)
	}

	sort.Slice(result, func(i, j int) bool {
		if result[i].Score != result[j].Score {
			return result[i].Score > result[j].Score
		}

		return result[i].RevisionID < result[j].RevisionID
	})

	return result
}

// Omitting any predicate releases a deliberately stronger excluded revision.
//
//nolint:funlen // Keep each stronger excluded revision next to the literal expected population.
func TestSearchSnapshotSemanticFiltersAndTime(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, base := semanticFixture()
	seedSemanticSource(t, s, g)
	before, after := g.ReadyAt.Add(-time.Nanosecond), g.ReadyAt.Add(time.Nanosecond)

	var chunks []semantic.ProjectionChunk

	mutations := []struct {
		id     string
		mutate func(*semantic.ProjectionChunk)
	}{
		{"eligible", func(c *semantic.ProjectionChunk) { c.Vector = []float32{0, 1} }},
		{"kind", func(c *semantic.ProjectionChunk) { c.Kind = memory.MemoryKindEvent }},
		{"category", func(c *semantic.ProjectionChunk) { c.Category = memory.AtomCategoryFact }},
		{"key", func(c *semantic.ProjectionChunk) { c.MemoryKey = "other" }},
		{"source", func(c *semantic.ProjectionChunk) { c.SourceIDs = []string{"other"} }},
		{"conversation", func(c *semantic.ProjectionChunk) { c.ConversationIDs = []string{"other"} }},
		{"observed-future", func(c *semantic.ProjectionChunk) { c.ObservedAt = &after }},
		{"valid-future", func(c *semantic.ProjectionChunk) { c.ValidFrom = &after }},
		{"ended", func(c *semantic.ProjectionChunk) { c.ValidUntil = &g.ReadyAt }},
		{"expired", func(c *semantic.ProjectionChunk) { c.ExpiresAt = &g.ReadyAt }},
		{"boundary", func(c *semantic.ProjectionChunk) {
			c.ObservedAt = &g.ReadyAt
			c.ValidFrom = &g.ReadyAt
			c.ValidUntil = &after
			c.ExpiresAt = &after
		}},
		{"past", func(c *semantic.ProjectionChunk) { c.ObservedAt = &before; c.ValidFrom = &before }},
	}
	for _, mutation := range mutations {
		chunk := base[0]
		chunk.ItemID = mutation.id
		chunk.RevisionID = mutation.id
		chunk.Category = memory.AtomCategoryDecision
		chunk.MemoryKey = "chosen"
		chunk.SourceIDs = []string{"source"}
		chunk.ConversationIDs = []string{"conversation"}
		mutation.mutate(&chunk)
		chunks = append(chunks, chunk)
	}

	stageSearchFixture(t, s, g, chunks)
	// Same key in another scope kind must never enter the selected population.
	other := g
	other.Scope.Kind = memory.ScopeKindGroup
	other.BuildID = "other-kind"
	seedSemanticSource(t, s, other)

	wrong := base[0]
	wrong.BuildID = other.BuildID
	wrong.Scope = other.Scope
	wrong.ItemID = "wrong-kind"
	wrong.RevisionID = "wrong-kind"
	stageSearchFixture(t, s, other, []semantic.ProjectionChunk{wrong})

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	kind, category := memory.MemoryKindState, memory.AtomCategoryDecision

	for _, tc := range []struct {
		name  string
		apply func(*memory.RecallRequest)
		want  []string
	}{
		{"time", func(*memory.RecallRequest) {}, []string{"boundary", "category", "conversation", "eligible", "key", "kind", "past", "source"}},
		{"kind", func(r *memory.RecallRequest) { r.Kind = &kind }, []string{"boundary", "category", "conversation", "eligible", "key", "past", "source"}},
		{"category", func(r *memory.RecallRequest) { r.Category = &category }, []string{"boundary", "conversation", "eligible", "key", "kind", "past", "source"}},
		{"key", func(r *memory.RecallRequest) { r.MemoryKey = "chosen" }, []string{"boundary", "category", "conversation", "eligible", "kind", "past", "source"}},
		{"source", func(r *memory.RecallRequest) { r.SourceID = "source" }, []string{"boundary", "category", "conversation", "eligible", "key", "kind", "past"}},
		{"conversation", func(r *memory.RecallRequest) { r.ConversationID = "conversation" }, []string{"boundary", "category", "eligible", "key", "kind", "past", "source"}},
		{"combined", func(r *memory.RecallRequest) {
			r.Kind = &kind
			r.Category = &category
			r.MemoryKey = "chosen"
			r.SourceID = "source"
			r.ConversationID = "conversation"
		}, []string{"boundary", "eligible", "past"}},
		{"empty", func(r *memory.RecallRequest) { r.SourceID = "missing" }, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 100, AsOf: &g.ReadyAt}
			tc.apply(&request)

			hits, counts, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 100)
			if err != nil {
				t.Fatal(err)
			}

			var got []string
			for _, hit := range hits {
				got = append(got, hit.RevisionID)
			}

			sort.Strings(got)

			if !reflect.DeepEqual(got, tc.want) || counts.ScannedChunks != len(tc.want) {
				t.Fatalf("got=%v count=%d want=%v", got, counts.ScannedChunks, tc.want)
			}
		})
	}
}

//nolint:dupl // Independent red-green reproductions keep each corruption outcome explicit.
func TestSearchSnapshotRejectsCoverageAndOrdinalCorruption(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	execTest(t, s.writer, "UPDATE semantic_chunk SET ordinal=1")

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
	if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("ordinal corruption=%v", err)
	}
}

func TestSearchSnapshotCancellationAndScope(t *testing.T) {
	s, _ := openTestStore(t, false)
	ctx, cancel := context.WithCancel(t.Context())

	snapshot, err := s.OpenSearchSnapshot(ctx, lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "query", Limit: 1}

	request.Scope.Kind = memory.ScopeKindPersonal
	if _, err = snapshot.SearchLexical(t.Context(), request, 1); !errors.Is(err, semantic.ErrInvalid) {
		t.Fatalf("cross-scope=%v", err)
	}

	cancel()

	if _, err = snapshot.SearchLexical(t.Context(), request, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled=%v", err)
	}

	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	if got := s.reader.Stats().InUse; got != 0 {
		t.Fatalf("leaked readers=%d", got)
	}
}

func TestSearchSnapshotVectorCorruptionMatrix(t *testing.T) {
	for _, tc := range []struct {
		name   string
		vector []float32
	}{
		{"zero", []float32{0, 0}}, {"norm", []float32{0.5, 0.5}},
		{"nan", []float32{float32(math.NaN()), 0}},
		{"positive infinity", []float32{float32(math.Inf(1)), 0}},
		{"negative infinity", []float32{float32(math.Inf(-1)), 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			execTest(t, s.writer, "UPDATE semantic_chunk SET embedding=?", encodeQueryVector(tc.vector))

			snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()
			// Even request-excluded corruption invalidates the selected complete build.
			request := memory.RecallRequest{Scope: g.Scope, Query: "query", SourceID: "absent", Limit: 1, AsOf: &g.ReadyAt}
			if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCorrupt) {
				t.Fatalf("corruption=%v", err)
			}
		})
	}
}

//nolint:dupl // Independent red-green reproductions keep each corruption outcome explicit.
func TestSearchSnapshotRejectsCorruptObservedTime(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	execTest(t, s.writer, "UPDATE semantic_chunk SET observed_at=zeroblob(17)")

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
	if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("corruption=%v", err)
	}
}

// Corruption can bypass schema constraints; the query must validate stored
// operands before calling a vector scalar, without relying on STRICT admission.
func TestSearchSnapshotRejectsStoredShapeCorruption(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		value any
	}{
		{"type", "UPDATE semantic_chunk SET embedding=?", "[1,0]"},
		{"malformed blob", "UPDATE semantic_chunk SET embedding=?", []byte{1, 2, 3}},
		{"wrong dimension", "UPDATE semantic_chunk SET dimensions=?", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
			execTest(t, s.writer, "ALTER TABLE semantic_chunk RENAME TO corrupt_original")
			execTest(t, s.writer, "CREATE TABLE semantic_chunk AS SELECT rowid,* FROM corrupt_original")
			execTest(t, s.writer, tc.query, tc.value)

			snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()

			request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
			if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCorrupt) {
				t.Fatalf("corruption=%v", err)
			}
		})
	}
}

// A logical tuple match cannot certify a different physical lexical build.
func TestSearchSnapshotPinsExactPhysicalSource(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)

	old, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()

	execTest(t, s.writer, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state)
 VALUES('new-physical',?,?,?,?,'committed')`, string(g.Scope.Kind), g.Scope.Key, g.LexicalGenerationID, encodeUint64(g.LexicalWatermark))
	execTest(t, s.writer, "UPDATE lexical_active SET physical_build_id='new-physical'")

	if state, found, err := old.SemanticState(); err != nil || !found || state.BuildID != g.BuildID {
		t.Fatalf("old state=%#v found=%v err=%v", state, found, err)
	}

	fresh, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	if _, found, err := fresh.SemanticState(); !found || !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("stale found=%v err=%v", found, err)
	}
}

func TestSearchSnapshotMissingAndDegradedStates(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	missing, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}

	if _, found, err := missing.SemanticState(); err != nil || found {
		t.Fatalf("missing found=%v err=%v", found, err)
	}

	if err = missing.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err = s.PublishDegraded(t.Context(), semantic.Degraded{Generation: g, SafeReason: semantic.ReasonUnavailable}, ""); err != nil {
		t.Fatal(err)
	}

	degraded, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer degraded.Close()

	if state, found, err := degraded.SemanticState(); err != nil || !found || state.Status != semantic.StatusDegraded {
		t.Fatalf("degraded=%#v found=%v err=%v", state, found, err)
	}
}

// Publication retires old rows, but both old channels and their metadata must
// remain coherent for an already-open reader in the same SQL transaction.
//
//nolint:cyclop,funlen // Check publication, both channels and metadata before/after atomic retirement.
func TestSearchSnapshotBothChannelsDuringPublication(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, base := semanticFixture()
	g.LexicalGenerationID = "logical"
	g.LexicalWatermark = math.MaxUint64
	seedLexical(t, s, "old-physical", g.Scope, []lexicalFixture{{id: "revision", raw: "needle", observed: &g.ReadyAt,
		sources: []string{"old-source"}, conversations: []string{"old-conversation"}}})
	execTest(t, s.writer, "UPDATE lexical_build SET generation_id='logical',record_count=1,source_count=1,conversation_count=1")

	first := base[0]
	first.LexicalGenerationID = g.LexicalGenerationID
	first.LexicalWatermark = g.LexicalWatermark
	first.SourceIDs = []string{"old-source"}
	first.ConversationIDs = []string{"old-conversation"}

	coverage := []semantic.CoverageRecord{{ItemID: first.ItemID, RevisionID: first.RevisionID, Revision: first.Revision, ContentHash: first.ContentHash, ChunkCount: 1}}
	registerSemanticPrivacy(t, s, g, coverage, []semantic.ProjectionChunk{first})

	if _, err := s.Stage(t.Context(), g, coverage, []semantic.ProjectionChunk{first}); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
		t.Fatal(err)
	}

	old, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()

	seedLexical(t, s, "new-physical", g.Scope, []lexicalFixture{{id: "new-revision", raw: "needle", observed: &g.ReadyAt,
		sources: []string{"new-source"}, conversations: []string{"new-conversation"}}})
	execTest(t, s.writer, "UPDATE lexical_build SET generation_id='logical',record_count=1,source_count=1,conversation_count=1 WHERE physical_build_id='new-physical'")

	next := g
	next.BuildID = "next"
	second := first
	second.BuildID = next.BuildID
	second.ItemID = "new-item"
	second.RevisionID = "new-revision"
	second.Vector = []float32{0, 1}
	second.SourceIDs = []string{"new-source"}
	second.ConversationIDs = []string{"new-conversation"}
	coverage[0].ItemID = second.ItemID

	coverage[0].RevisionID = second.RevisionID
	registerSemanticPrivacy(t, s, next, coverage, []semantic.ProjectionChunk{second})

	if _, err = s.Stage(t.Context(), next, coverage, []semantic.ProjectionChunk{second}); err != nil {
		t.Fatal(err)
	}

	if err = s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: g.BuildID, ExpectedActiveEpoch: 1}); err != nil {
		t.Fatal(err)
	}

	fresh, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()

	for _, tc := range []struct {
		snapshot             semantic.SearchSnapshot
		revision, build      string
		source, conversation string
		epoch                uint64
		score                float64
	}{
		{old, "revision", g.BuildID, "old-source", "old-conversation", 1, 1},
		{fresh, "new-revision", next.BuildID, "new-source", "new-conversation", 2, 0},
	} {
		request := memory.RecallRequest{Scope: g.Scope, Query: "needle", Limit: 10, AsOf: &g.ReadyAt}
		request.SourceID = tc.source
		request.ConversationID = tc.conversation

		lexical, err := tc.snapshot.SearchLexical(t.Context(), request, 10)
		if err != nil || len(lexical) != 1 || lexical[0].RevisionID != tc.revision {
			t.Fatalf("lexical=%#v err=%v", lexical, err)
		}

		dense, _, err := tc.snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 10)
		if err != nil || len(dense) != 1 || dense[0].RevisionID != tc.revision || dense[0].Score != tc.score {
			t.Fatalf("dense=%#v err=%v", dense, err)
		}

		state, found, err := tc.snapshot.SemanticState()
		if err != nil || !found || state.BuildID != tc.build || state.PublicationEpoch != tc.epoch {
			t.Fatalf("state=%#v found=%v err=%v", state, found, err)
		}

		request.SourceID = "absent"
		if hits, err := tc.snapshot.SearchLexical(t.Context(), request, 10); err != nil || len(hits) != 0 {
			t.Fatalf("excluded lexical membership=%#v err=%v", hits, err)
		}

		if hits, _, err := tc.snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 10); err != nil || len(hits) != 0 {
			t.Fatalf("excluded dense membership=%#v err=%v", hits, err)
		}
	}
}

func TestSearchSnapshotCallCancellationAndAutomaticDrain(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	ctx, cancel := context.WithCancel(t.Context())

	snapshot, err := s.OpenSearchSnapshot(ctx, g.Scope)
	if err != nil {
		t.Fatal(err)
	}

	call, cancelCall := context.WithCancel(t.Context())
	cancelCall()

	request := memory.RecallRequest{Scope: g.Scope, Query: "needle", Limit: 1, AsOf: &g.ReadyAt}
	if _, _, err = snapshot.SearchSemantic(call, request, [][]float32{{1, 0}}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("call cancellation=%v", err)
	}
	// A canceled call does not consume the caller-owned snapshot.
	if hits, _, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); err != nil || len(hits) != 1 {
		t.Fatalf("after canceled call=%#v %v", hits, err)
	}

	cancel()
	// No explicit Close: cancellation must release the snapshot lifecycle lease.
	deadline, done := context.WithTimeout(t.Context(), time.Second)
	defer done()

	if err = s.CloseContext(deadline); err != nil {
		t.Fatalf("automatic drain=%v", err)
	}
}

func TestSearchSnapshotRejectsOversizedSelectedBuildBeforeVectorScalars(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
	execTest(t, s.writer, "ALTER TABLE semantic_chunk RENAME TO corrupt_original")
	execTest(t, s.writer, "CREATE TABLE semantic_chunk AS SELECT rowid,* FROM corrupt_original")
	execTest(t, s.writer, `WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<8192)
 INSERT INTO semantic_chunk SELECT corrupt_original.rowid,corrupt_original.* FROM corrupt_original CROSS JOIN n`)
	execTest(t, s.writer, "UPDATE semantic_chunk SET embedding=x'01'")

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
	if _, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCapacity) {
		t.Fatalf("oversized=%v", err)
	}
}

// The explicit channel depth, rather than public request.Limit, controls recall
// candidates; invalid dimensions, unit norms and bounds fail before SQL ranking.
//

func TestSearchSnapshotSemanticChannelAndQueryBounds(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, base := semanticFixture()
	seedSemanticSource(t, s, g)

	chunks := make([]semantic.ProjectionChunk, 105)
	for i := range chunks {
		chunks[i] = base[0]
		chunks[i].RevisionID = fmt.Sprintf("r%03d", i)
		chunks[i].ItemID = chunks[i].RevisionID
	}

	stageSearchFixture(t, s, g, chunks)

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}

	hits, counts, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 100)
	if err != nil || len(hits) != 100 || counts.ScannedChunks != 105 || hits[0].RevisionID != "r000" || hits[99].RevisionID != "r099" {
		t.Fatalf("cap/tie=%#v counts=%#v err=%v", hits, counts, err)
	}

	for _, tc := range []struct {
		name    string
		queries [][]float32
		limit   int
	}{
		{"zero candidates", [][]float32{{1, 0}}, 0}, {"too many candidates", [][]float32{{1, 0}}, 101},
		{"no queries", nil, 1}, {"too many queries", [][]float32{{1, 0}, {1, 0}, {1, 0}, {1, 0}, {1, 0}}, 1},
		{"dimension", [][]float32{{1}}, 1}, {"norm", [][]float32{{2, 0}}, 1},
		{"nan", [][]float32{{float32(math.NaN()), 0}}, 1}, {"infinity", [][]float32{{float32(math.Inf(1)), 0}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := snapshot.SearchSemantic(t.Context(), request, tc.queries, tc.limit); !errors.Is(err, semantic.ErrInvalid) {
				t.Fatalf("invalid=%v", err)
			}
		})
	}
}
