package sqlite

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
)

func semanticFixture() (semantic.Generation, []semantic.CoverageRecord, []semantic.ProjectionChunk) {
	at := time.Date(2020, 1, 2, 3, 4, 5, 6, time.UTC)
	g := semantic.Generation{
		Scope:               memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:semantic"},
		BuildID:             "semantic-build",
		LexicalGenerationID: "lexical-build",
		LexicalWatermark:    1,
		ModelSpace:          strings.Repeat("a", 64),
		Dimensions:          2,
		ReadyAt:             at,
	}
	c := semantic.ProjectionChunk{
		Scope:               g.Scope,
		BuildID:             g.BuildID,
		LexicalGenerationID: g.LexicalGenerationID,
		LexicalWatermark:    1,
		ModelSpace:          g.ModelSpace,
		Dimensions:          2,
		ItemID:              "item",
		RevisionID:          "revision",
		Revision:            1,
		ContentHash:         strings.Repeat("b", 64),
		InputHash:           strings.Repeat("c", 64),
		ScopeChangeSeq:      1,
		Kind:                memory.MemoryKindState,
		CreatedAt:           at,
		ObservedAt:          &at,
		Vector:              []float32{1, 0},
	}

	return g, []semantic.CoverageRecord{
			{ItemID: c.ItemID, RevisionID: c.RevisionID, Revision: 1, ContentHash: c.ContentHash, ChunkCount: 1},
		}, []semantic.ProjectionChunk{
			c,
		}
}

func TestSemanticStageIsCompleteButUnpublished(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, coverage, chunks := semanticFixture()
	seedSemanticSource(t, s, g)

	state, err := s.Stage(t.Context(), g, coverage, chunks)
	if err != nil {
		t.Fatal(err)
	}

	if state.ChunkCount != 1 || state.RecordCount != 1 || state.EncodedBytes == 0 {
		t.Fatalf("invalid staged state: %#v", state)
	}

	var complete int
	if err = s.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", g.BuildID).
		Scan(&complete); err != nil ||
		complete != 1 {
		t.Fatalf("complete=%d err=%v", complete, err)
	}

	if _, found, err := s.ActiveState(t.Context(), g.Scope); err != nil || found {
		t.Fatalf("unpublished state found=%v err=%v", found, err)
	}

	if _, err = s.Stage(t.Context(), g, coverage, chunks); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("duplicate stage: %v", err)
	}
}

func TestSemanticStageRejectsInvalidBeforeWriting(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*semantic.Generation, []semantic.CoverageRecord, []semantic.ProjectionChunk)
	}{
		{"missing observed", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].ObservedAt = nil
		}},
		{"bad vector", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].Vector[0] = 2
		}},
		{"bad ordinal", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].Ordinal = 1
		}},
		{"missing coverage", func(_ *semantic.Generation, r []semantic.CoverageRecord, _ []semantic.ProjectionChunk) {
			r[0].ChunkCount = 2
		}},
		{"nul identity", func(_ *semantic.Generation, _ []semantic.CoverageRecord, c []semantic.ProjectionChunk) {
			c[0].ItemID = "item\x00"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			test.mutate(&g, r, c)

			if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrInvalid) {
				t.Fatalf("stage: %v", err)
			}

			var n int
			if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build").
				Scan(&n); err != nil ||
				n != 0 {
				t.Fatalf("writes=%d err=%v", n, err)
			}
		})
	}
}

func TestSemanticStageCanceledLeavesNoCompleteState(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := s.Stage(ctx, g, r, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("stage: %v", err)
	}
}

func seedSemanticSource(t *testing.T, s *Store, g semantic.Generation) {
	t.Helper()

	_, coverage, chunks := semanticFixture()
	registerSemanticPrivacy(t, s, g, coverage, chunks)
	execTest(
		t,
		s.writer,
		`INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state)
		 VALUES(?,?,?,?,?,'committed')`,
		g.BuildID+"-physical",
		string(g.Scope.Kind),
		g.Scope.Key,
		g.LexicalGenerationID,
		encodeUint64(g.LexicalWatermark),
	)
	execTest(
		t,
		s.writer,
		`INSERT INTO lexical_active(scope_kind,scope_key,physical_build_id) VALUES(?,?,?)`,
		string(g.Scope.Kind),
		g.Scope.Key,
		g.BuildID+"-physical",
	)
}

// activateSemanticFixture is fixture setup only; TASK010 owns publication CAS.
func activateSemanticFixture(t *testing.T, s *Store, g semantic.Generation) {
	t.Helper()
	execTest(t, s.writer, "UPDATE semantic_build SET epoch=? WHERE build_id=?", encodeUint64(1), g.BuildID)
	execTest(
		t,
		s.writer,
		"INSERT INTO semantic_active(scope_kind,scope_key,build_id,epoch) VALUES(?,?,?,?)",
		string(g.Scope.Kind),
		g.Scope.Key,
		g.BuildID,
		encodeUint64(1),
	)
}

//nolint:cyclop // End-to-end assertions cover owned vectors, codec and every view component.
func TestSemanticReusableOwnsVectorsAndRequiresExactView(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)

	reused, found, err := s.LoadReusable(t.Context(), g.View())
	if err != nil || !found || len(reused.Chunks) != 1 {
		t.Fatalf("reuse=%#v found=%v err=%v", reused, found, err)
	}

	if reused.Chunks[0].ItemID != "item" || reused.Chunks[0].InputHash != strings.Repeat("c", 64) ||
		reused.Chunks[0].Vector[0] != 1 {
		t.Fatalf("reuse identity/vector=%#v", reused.Chunks[0])
	}

	reused.Chunks[0].Vector[0] = 0

	again, found, err := s.LoadReusable(t.Context(), g.View())
	if err != nil || !found || again.Chunks[0].Vector[0] != 1 {
		t.Fatalf("mutable reuse: %#v %v %v", again, found, err)
	}

	for _, change := range []func(*semantic.View){
		func(v *semantic.View) { v.ModelSpace = strings.Repeat("f", 64) },
		func(v *semantic.View) { v.Dimensions++ },
		func(v *semantic.View) { v.LexicalWatermark++ },
		func(v *semantic.View) { v.LexicalGenerationID = "other" },
	} {
		view := g.View()
		change(&view)

		if _, found, err = s.LoadReusable(t.Context(), view); err != nil || found {
			t.Fatalf("incompatible reuse: %v %v", found, err)
		}
	}

	if ready, err := s.ObservedMetadataReady(t.Context(), g.View()); err != nil || !ready {
		t.Fatalf("observed ready=%v err=%v", ready, err)
	}

	var encoded []byte
	if err = s.reader.QueryRowContext(t.Context(), "SELECT embedding FROM semantic_chunk").Scan(&encoded); err != nil {
		t.Fatal(err)
	}

	if len(encoded) != 8 || binary.LittleEndian.Uint32(encoded) != math.Float32bits(1) {
		t.Fatalf("embedding=%x", encoded)
	}
}

func TestSemanticCompleteBuildingStateIsNeverReady(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	execTest(t, s.writer, "UPDATE semantic_build SET status='building' WHERE build_id=?", g.BuildID)

	if _, _, err := s.ActiveState(t.Context(), g.Scope); !errors.Is(err, semantic.ErrCorrupt) {
		t.Fatalf("building active state: %v", err)
	}
}

//nolint:cyclop,gocognit,funlen // The fault matrix shares real reopen and scope-isolation assertions.
func TestSemanticCorruptionRevokesOnlyBadBuildOnEnabledReopen(t *testing.T) {
	for _, test := range []struct {
		name, query string
		args        []any
	}{
		{"nan", "UPDATE semantic_chunk SET embedding=? WHERE build_id=?", []any{[]byte{0, 0, 192, 127, 0, 0, 0, 0}}},
		{"norm", "UPDATE semantic_chunk SET embedding=? WHERE build_id=?", []any{[]byte{0, 0, 0, 64, 0, 0, 0, 0}}},
		{"input hash", "UPDATE semantic_chunk SET input_hash='malformed' WHERE build_id=?", nil},
		{"coverage hash", "UPDATE semantic_build SET coverage_hash=? WHERE build_id=?", []any{strings.Repeat("f", 64)}},
		{"count", "UPDATE semantic_build SET chunk_count=2 WHERE build_id=?", nil},
		{"ordinal", "UPDATE semantic_chunk SET ordinal=1 WHERE build_id=?", nil},
		{"observed timestamp", "UPDATE semantic_chunk SET observed_at=zeroblob(17) WHERE build_id=?", nil},
		{"model", "UPDATE semantic_build SET model_space='malformed' WHERE build_id=?", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			s, dir := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			good := g
			good.BuildID = "good"
			good.Scope.Key = "test:good"
			goodChunks := semantic.CloneChunks(c)
			goodChunks[0].BuildID = good.BuildID
			goodChunks[0].Scope = good.Scope
			seedSemanticSource(t, s, good)

			if _, err := s.Stage(t.Context(), good, r, goodChunks); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, good)

			pending := g

			pending.BuildID = "pending"
			if err := s.claimSemanticBuild(
				t.Context(),
				semantic.State{
					Scope:               pending.Scope,
					BuildID:             pending.BuildID,
					LexicalGenerationID: pending.LexicalGenerationID,
					LexicalWatermark:    1,
					ModelSpace:          pending.ModelSpace,
					Dimensions:          2,
					ReadyAt:             pending.ReadyAt,
				},
			); err != nil {
				t.Fatal(err)
			}

			args := append(append([]any(nil), test.args...), g.BuildID)
			execTest(t, s.writer, test.query, args...)

			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()

			if _, _, err = reopened.ActiveState(t.Context(), g.Scope); !errors.Is(err, semantic.ErrCorrupt) {
				t.Fatalf("bad active: %v", err)
			}

			if _, found, err := reopened.LoadReusable(t.Context(), good.View()); err != nil || !found {
				t.Fatalf("good reuse=%v %v", found, err)
			}

			var complete, n int

			const pendingQuery = "SELECT complete FROM semantic_build WHERE build_id='pending'"
			if err = reopened.reader.QueryRowContext(t.Context(), pendingQuery).
				Scan(&complete); err != nil ||
				complete != 0 {
				t.Fatalf("pending=%d %v", complete, err)
			}

			const badRowsQuery = "SELECT count(*) FROM semantic_chunk WHERE build_id=?"
			if err = reopened.reader.QueryRowContext(t.Context(), badRowsQuery, g.BuildID).
				Scan(&n); err != nil ||
				n != 1 {
				t.Fatalf("bad rows preserved=%d %v", n, err)
			}

			if err = reopened.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_active").
				Scan(&n); err != nil ||
				n != 2 {
				t.Fatalf("lexical unaffected=%d %v", n, err)
			}
		})
	}
}

func TestSemanticStagePinsExactLexicalSource(t *testing.T) {
	s, _ := openTestStore(t, true)

	g, r, c := semanticFixture()
	registerSemanticPrivacy(t, s, g, r, c)

	if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("missing source: %v", err)
	}

	seedSemanticSource(t, s, g)
	execTest(t, s.writer, "UPDATE lexical_build SET revoked=1")

	if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("revoked source: %v", err)
	}

	execTest(t, s.writer, "UPDATE lexical_build SET revoked=0,watermark=?", encodeUint64(2))

	if _, err := s.Stage(t.Context(), g, r, c); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("wrong source: %v", err)
	}
}

func TestSemanticStageRejectsForgedCompleteLexicalSource(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)
	execTest(t, s.writer, "UPDATE lexical_build SET record_count=1")

	if _, err := s.Stage(t.Context(), g, r, c); err == nil {
		t.Fatal("forged complete lexical marker admitted semantic stage")
	}

	var n int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build").
		Scan(&n); err != nil ||
		n != 0 {
		t.Fatalf("claim after corrupt source=%d %v", n, err)
	}
}
