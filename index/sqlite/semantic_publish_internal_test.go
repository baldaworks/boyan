//go:build integration

//nolint:cyclop,gocognit,funlen,lll // Real SQL fault matrices retain independent state and rollback assertions.
package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	sqlitedriver "modernc.org/sqlite"
)

func TestSemanticCanonicalForgetLeavesRepairableCAS(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	first := stageNamedSemantic(t, s, "first")
	if err := s.Publish(t.Context(), semantic.Publication{View: first.View(), BuildID: first.BuildID}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err := applier.ScrubCanonicalForget(t.Context(), g.Scope, nil, []string{"revision"}); err != nil {
		t.Fatal(err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.Status != semantic.StatusDegraded || state.PublicationEpoch != 1 {
		t.Fatalf("repair CAS=%#v %v %v", state, found, err)
	}

	next := stageNamedSemantic(t, s, "next")
	if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: state.BuildID, ExpectedActiveEpoch: state.PublicationEpoch}); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticRetirementRejectsCorruptPredecessor(t *testing.T) {
	for _, mutation := range []string{
		"UPDATE semantic_build SET complete=0 WHERE build_id='first'",
		"UPDATE semantic_build SET chunk_count=0,revision_count=0 WHERE build_id='first'",
		"UPDATE semantic_build SET reason='private prose' WHERE build_id='first'",
		"UPDATE semantic_build SET encoded_bytes=1 WHERE build_id='first'",
		"UPDATE semantic_chunk SET ordinal=1 WHERE build_id='first'",
		"UPDATE semantic_build SET coverage_hash='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa' WHERE build_id='first'",
	} {
		t.Run(mutation, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, _, _ := semanticFixture()
			seedSemanticSource(t, s, g)
			first := stageNamedSemantic(t, s, "first")

			next := stageNamedSemantic(t, s, "next")
			if err := s.Publish(t.Context(), semantic.Publication{View: first.View(), BuildID: first.BuildID}); err != nil {
				t.Fatal(err)
			}

			execTest(t, s.writer, mutation)

			if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: 1}); !errors.Is(err, semantic.ErrCorrupt) {
				t.Fatalf("corrupt prior publication=%v", err)
			}

			checkSemanticRows(t, s, "first", 1)
			checkSemanticRows(t, s, "next", 1)
		})
	}
}

func TestSemanticScrubRejectsReusedCandidateRow(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	first := stageNamedSemantic(t, s, "first")
	if err := s.Publish(t.Context(), semantic.Publication{View: first.View(), BuildID: first.BuildID}); err != nil {
		t.Fatal(err)
	}

	predicate, args := scrubPredicate(g.Scope, []string{"revision"}, false, true)
	query := `SELECT r.rowid AS id,1 AS bytes,r.build_id AS owner,r.revision_id AS revision FROM semantic_chunk r JOIN semantic_build b USING(build_id) WHERE ` + predicate + ` ORDER BY r.rowid LIMIT 128`

	rows, err := s.scrubCandidates(t.Context(), query, args)
	if err != nil || len(rows) != 1 {
		t.Fatalf("candidate=%#v %v", rows, err)
	}

	marker := g

	marker.BuildID = "marker"
	if _, err := s.PublishDegraded(t.Context(), semantic.Degraded{Generation: marker, SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: 1}, first.BuildID); err != nil {
		t.Fatal(err)
	}

	next, coverage, chunks := semanticFixture()
	next.BuildID = "unrelated"
	coverage[0].RevisionID = "unrelated"
	chunks[0].RevisionID = "unrelated"

	chunks[0].BuildID = next.BuildID
	registerSemanticPrivacy(t, s, next, coverage, chunks)

	if _, err := s.Stage(t.Context(), next, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: marker.BuildID, ExpectedActiveEpoch: 2}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err := applier.deleteScrubRows(t.Context(), rows, true); err != nil {
		t.Fatal(err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.Status != semantic.StatusReady || state.BuildID != next.BuildID {
		t.Fatalf("ABA revoked newer winner=%#v %v %v", state, found, err)
	}
}

func TestSemanticGarbageCancellationResumesAfterReopen(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	calls := 0

	name := fmt.Sprintf("cancel_semantic_gc_%d", semanticCancelSequence.Add(1))
	if err := sqlitedriver.RegisterScalarFunction(name, 0, func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
		calls++
		if calls == 129 {
			cancel()
		}

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	s, dir := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	active := stageNamedSemantic(t, s, "active")
	if err := s.Publish(t.Context(), semantic.Publication{View: active.View(), BuildID: active.BuildID}); err != nil {
		t.Fatal(err)
	}

	g.BuildID = "orphan"
	r[0].ChunkCount = 257

	chunks := make([]semantic.ProjectionChunk, 257)
	for i := range chunks {
		chunks[i] = c[0]
		chunks[i].BuildID = g.BuildID
		chunks[i].Ordinal = uint32(i)
	}

	if _, err := s.Stage(t.Context(), g, r, chunks); err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "UPDATE semantic_build SET complete=0 WHERE build_id='orphan'")
	execTest(t, s.writer, "CREATE TRIGGER cancel_gc AFTER DELETE ON semantic_chunk WHEN old.build_id='orphan' BEGIN SELECT "+name+"(); END")

	if err := s.CollectGarbage(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GC cancel=%v", err)
	}

	var n int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_chunk WHERE build_id='orphan'").Scan(&n); err != nil || n != 129 {
		t.Fatalf("page rollback rows=%d err=%v", n, err)
	}

	execTest(t, s.writer, "DROP TRIGGER cancel_gc")

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()

	if err := reopened.RepairSemanticOrphans(t.Context()); err != nil {
		t.Fatal(err)
	}

	checkSemanticRows(t, reopened, "orphan", 0)

	if _, found, err := reopened.LoadReusable(t.Context(), active.View()); err != nil || !found {
		t.Fatalf("active preserved=%v %v", found, err)
	}
}

func TestSemanticEmptyPublicationAndCoverageOnlyRetirement(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
		t.Fatal(err)
	}
	// Historical incomplete nonactive builds can retain coverage without chunks.
	seedZeroCoverage(t, s, "coverage-only", g.Scope, "revision")
	execTest(t, s.writer, "UPDATE semantic_build SET complete=0 WHERE build_id='coverage-only'")

	applier, err := NewSQLiteCanonicalApplier(s, &applyReader{}, fixtureCanonicalPrivacy(t, s, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	if err := applier.ScrubCanonicalForget(t.Context(), g.Scope, nil, []string{"revision"}); err != nil {
		t.Fatal(err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.Status != semantic.StatusReady {
		t.Fatalf("empty active preserved=%#v %v %v", state, found, err)
	}

	next := stageNamedSemantic(t, s, "next")
	if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: g.BuildID, ExpectedActiveEpoch: 1}); err != nil {
		t.Fatal(err)
	}

	checkSemanticRows(t, s, g.BuildID, 0)
}

func TestSemanticPublicationEpochGrowthPreservesExactAdmission(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, c := semanticFixture()
	seedSemanticSource(t, s, g)

	active := stageNamedSemantic(t, s, "active")
	if err := s.Publish(t.Context(), semantic.Publication{View: active.View(), BuildID: active.BuildID}); err != nil {
		t.Fatal(err)
	}

	execTest(t, s.writer, "DELETE FROM semantic_active")
	execTest(t, s.writer, "UPDATE semantic_build SET epoch=? WHERE build_id=?", encodeUint64(9), active.BuildID)
	execTest(t, s.writer, "INSERT INTO semantic_active(scope_kind,scope_key,build_id,epoch) VALUES(?,?,?,?)", string(g.Scope.Kind), g.Scope.Key, active.BuildID, encodeUint64(9))
	g.BuildID = "boundary"
	g.Dimensions = 4096
	c[0].BuildID = g.BuildID
	c[0].Dimensions = 4096
	c[0].Vector = make([]float32, 4096)
	c[0].Vector[0] = 1

	encoded, err := semantic.EncodeChunk(c[0])
	if err != nil {
		t.Fatal(err)
	}

	count := (semantic.ProjectionByteLimit - 100000) / (len(encoded) + semantic.LogicalChunkKeyBytes)
	chunks := make([]semantic.ProjectionChunk, count)
	r := make([]semantic.CoverageRecord, count)

	for i := range chunks {
		chunks[i] = c[0]
		chunks[i].ItemID = fmt.Sprintf("item-%04d", i)
		chunks[i].RevisionID = fmt.Sprintf("revision-%04d", i)
		r[i] = semantic.CoverageRecord{ItemID: chunks[i].ItemID, RevisionID: chunks[i].RevisionID, Revision: 1, ContentHash: chunks[i].ContentHash, ChunkCount: 1}
	}

	base, _, err := semantic.PrepareProjection(t.Context(), g, r, chunks)
	if err != nil {
		t.Fatal(err)
	}

	deficit := int(uint64(semantic.ProjectionByteLimit) - base.EncodedBytes)
	last := len(chunks) - 1

	before, err := semantic.EncodeChunk(chunks[last])
	if err != nil {
		t.Fatal(err)
	}

	padded := false

	for n := 1; n <= 256; n++ {
		ids := make([]string, n)
		for i := range ids {
			ids[i] = fmt.Sprintf("%03d", i)
		}

		chunks[last].SourceIDs = ids

		value, encodeErr := semantic.EncodeChunk(chunks[last])
		if encodeErr != nil {
			t.Fatal(encodeErr)
		}

		extra := deficit - (len(value) - len(before))
		if extra < 0 || extra > n*509 {
			continue
		}

		for i := range ids {
			addition := min(extra, 509)
			ids[i] += strings.Repeat("x", addition)
			extra -= addition
		}

		padded = true

		break
	}

	if !padded {
		t.Fatalf("padding deficit=%d", deficit)
	}

	registerSemanticPrivacy(t, s, g, r, chunks)

	state, err := s.Stage(t.Context(), g, r, chunks)
	if err != nil {
		t.Fatal(err)
	}

	if state.EncodedBytes != uint64(semantic.ProjectionByteLimit) {
		t.Fatalf("boundary bytes=%d", state.EncodedBytes)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID, ExpectedActiveBuildID: active.BuildID, ExpectedActiveEpoch: 9}); !errors.Is(err, semantic.ErrCapacity) {
		t.Fatalf("epoch growth=%v", err)
	}

	state, found, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil || !found || state.BuildID != active.BuildID || state.PublicationEpoch != 9 {
		t.Fatalf("rollback=%#v %v %v", state, found, err)
	}

	checkSemanticRows(t, s, g.BuildID, 1)
}

// Real SQL triggers cancel after the pointer changed or amid cascading
// retirement. Removing the shared transaction would expose either partial state.
func TestSemanticPublicationCancellationRollsBackPointerAndRetirement(t *testing.T) {
	for _, boundary := range []string{"pointer", "chunk", "coverage", "degrade", "invalidate"} {
		t.Run(boundary, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			name := fmt.Sprintf("cancel_semantic_publish_%d", semanticCancelSequence.Add(1))
			if err := sqlitedriver.RegisterScalarFunction(name, 0, func(*sqlitedriver.FunctionContext, []driver.Value) (driver.Value, error) {
				cancel()

				return int64(0), nil
			}); err != nil {
				t.Fatal(err)
			}

			s, _ := openTestStore(t, true)
			g, coverage, chunks := semanticFixture()
			seedSemanticSource(t, s, g)
			// Exercise nontrivial FK membership fanout during atomic retirement.
			chunks[0].SourceIDs = make([]string, 256)
			for i := range chunks[0].SourceIDs {
				chunks[0].SourceIDs[i] = fmt.Sprintf("%04d", i)
			}

			registerSemanticPrivacy(t, s, g, coverage, chunks)

			if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
				t.Fatal(err)
			}

			if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); err != nil {
				t.Fatal(err)
			}

			next := stageNamedSemantic(t, s, "next")
			stageNamedSemantic(t, s, "pending")

			trigger := "AFTER UPDATE ON semantic_active"
			if boundary == "chunk" || boundary == "degrade" || boundary == "invalidate" {
				trigger = "AFTER DELETE ON semantic_source"
			}

			if boundary == "coverage" {
				trigger = "AFTER DELETE ON semantic_coverage"
			}

			execTest(t, s.writer, "CREATE TRIGGER cancel_publish "+trigger+" BEGIN SELECT "+name+"(); END")

			var err error

			switch boundary {
			case "degrade":
				marker := g
				marker.BuildID = "degraded"
				_, err = s.PublishDegraded(ctx, semantic.Degraded{Generation: marker, SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: 1}, g.BuildID)
			case "invalidate":
				err = s.InvalidatePublication(ctx, g.Scope, g.BuildID, 1)
			default:
				err = s.Publish(ctx, semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: g.BuildID, ExpectedActiveEpoch: 1})
			}

			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancel=%v", err)
			}

			state, found, err := s.ActiveState(t.Context(), g.Scope)
			if err != nil || !found || state.BuildID != g.BuildID || state.PublicationEpoch != 1 || state.Status != semantic.StatusReady {
				t.Fatalf("rollback state=%#v %v %v", state, found, err)
			}

			if _, found, err := s.LoadReusable(t.Context(), g.View()); err != nil || !found {
				t.Fatalf("old data lost=%v %v", found, err)
			}

			checkSemanticRows(t, s, "next", 1)
			checkSemanticRows(t, s, "pending", 1)
		})
	}
}

func TestSemanticDegradedControlFailsClosed(t *testing.T) {
	for _, query := range []string{
		"UPDATE semantic_build SET reason='raw private prose'",
		"UPDATE semantic_build SET revision_count=1",
		"UPDATE semantic_build SET encoded_bytes=1",
		"UPDATE semantic_build SET coverage_hash='wrong'",
	} {
		t.Run(query, func(t *testing.T) {
			s, dir := openTestStore(t, true)
			g, _, _ := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.PublishDegraded(t.Context(), semantic.Degraded{Generation: g, SafeReason: semantic.ReasonUnavailable}, ""); err != nil {
				t.Fatal(err)
			}

			execTest(t, s.writer, query)

			if state, _, err := s.ActiveState(t.Context(), g.Scope); !errors.Is(err, semantic.ErrCorrupt) || state.SafeReason != "" {
				t.Fatalf("corrupt exposed: %#v %v", state, err)
			}

			if err := s.Close(); err != nil {
				t.Fatal(err)
			}

			reopened, err := Open(t.Context(), dir, Options{EmbeddingsEnabled: true})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reopened.Close() }()

			var complete int
			if err := reopened.reader.QueryRowContext(t.Context(), "SELECT complete FROM semantic_build WHERE build_id=?", g.BuildID).Scan(&complete); err != nil || complete != 0 {
				t.Fatalf("startup certificate=%d %v", complete, err)
			}
		})
	}
}

func TestSemanticCompetingPublishersAndOverflow(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)
	a := stageNamedSemantic(t, s, "a")
	b := stageNamedSemantic(t, s, "b")
	start := make(chan struct{})
	results := make(chan error, 2)

	var workers sync.WaitGroup
	for _, candidate := range []semantic.Generation{a, b} {
		workers.Go(func() {
			<-start

			results <- s.Publish(t.Context(), semantic.Publication{View: candidate.View(), BuildID: candidate.BuildID})
		})
	}

	close(start)
	workers.Wait()
	close(results)

	successes, conflicts := 0, 0

	for err := range results {
		if err == nil {
			successes++
		} else if errors.Is(err, semantic.ErrConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}

	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}

	state, _, err := s.ActiveState(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	// Update parent and child together without violating the immediate FK.
	execTest(t, s.writer, "DELETE FROM semantic_active")
	execTest(t, s.writer, "UPDATE semantic_build SET epoch=? WHERE build_id=?", encodeUint64(^uint64(0)), state.BuildID)
	execTest(t, s.writer, "INSERT INTO semantic_active(scope_kind,scope_key,build_id,epoch) VALUES(?,?,?,?)", string(g.Scope.Kind), g.Scope.Key, state.BuildID, encodeUint64(^uint64(0)))

	next := stageNamedSemantic(t, s, "next")
	if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: state.BuildID, ExpectedActiveEpoch: ^uint64(0)}); !errors.Is(err, semantic.ErrCapacity) {
		t.Fatalf("overflow=%v", err)
	}
}

func TestSemanticPublicationRejectsChangedPhysicalSourceAndModel(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)
	g = stageNamedSemantic(t, s, "pending")
	wrong := g.View()

	wrong.Dimensions++
	if err := s.Publish(t.Context(), semantic.Publication{View: wrong, BuildID: g.BuildID}); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("wrong model: %v", err)
	}

	execTest(t, s.writer, `INSERT INTO lexical_build(physical_build_id,scope_kind,scope_key,generation_id,watermark,state) VALUES('new',?,?,?,?, 'committed')`, string(g.Scope.Kind), g.Scope.Key, g.LexicalGenerationID, encodeUint64(g.LexicalWatermark))
	execTest(t, s.writer, "UPDATE lexical_active SET physical_build_id='new'")

	if err := s.Publish(t.Context(), semantic.Publication{View: g.View(), BuildID: g.BuildID}); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("same tuple changed physical: %v", err)
	}
}

func TestSemanticDegradationAndPreciseInvalidation(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	first := stageNamedSemantic(t, s, "first")
	if err := s.Publish(t.Context(), semantic.Publication{View: first.View(), BuildID: first.BuildID}); err != nil {
		t.Fatal(err)
	}

	degraded := g
	degraded.BuildID = "degraded"

	state, err := s.PublishDegraded(t.Context(), semantic.Degraded{Generation: degraded, SafeReason: semantic.ReasonUnavailable, ExpectedActiveEpoch: 1}, first.BuildID)
	if err != nil {
		t.Fatal(err)
	}

	if state.Status != semantic.StatusDegraded || state.PublicationEpoch != 2 || state.ChunkCount != 0 {
		t.Fatalf("marker: %#v", state)
	}

	if err := s.InvalidatePublication(t.Context(), g.Scope, first.BuildID, 1); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("stale invalidation: %v", err)
	}

	if _, found, err := s.ActiveState(t.Context(), g.Scope); err != nil || !found {
		t.Fatalf("winner: %v %v", found, err)
	}

	if err := s.InvalidatePublication(t.Context(), g.Scope, state.BuildID, 2); err != nil {
		t.Fatal(err)
	}

	if state, found, err := s.ActiveState(t.Context(), g.Scope); err != nil || !found || state.Status != semantic.StatusDegraded {
		t.Fatalf("invalidated: %#v %v %v", state, found, err)
	}

	next := stageNamedSemantic(t, s, "next")
	if err := s.Publish(t.Context(), semantic.Publication{View: next.View(), BuildID: next.BuildID, ExpectedActiveBuildID: state.BuildID, ExpectedActiveEpoch: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticGarbageOwnershipAndStartupRepair(t *testing.T) {
	s, _ := openTestStore(t, true)
	g, _, _ := semanticFixture()
	seedSemanticSource(t, s, g)

	active := stageNamedSemantic(t, s, "active")
	if err := s.Publish(t.Context(), semantic.Publication{View: active.View(), BuildID: active.BuildID}); err != nil {
		t.Fatal(err)
	}

	stageNamedSemantic(t, s, "pending")

	for _, id := range []string{"orphan", "live"} {
		state := semantic.State{Scope: g.Scope, BuildID: id, LexicalGenerationID: g.LexicalGenerationID, LexicalWatermark: 1, ModelSpace: g.ModelSpace, Dimensions: 2, ReadyAt: g.ReadyAt}
		if err := s.claimSemanticBuild(t.Context(), state); err != nil {
			t.Fatal(err)
		}
	}

	s.semanticStaging.Store("live", struct{}{})
	defer s.semanticStaging.Delete("live")

	canceled, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.CollectGarbage(canceled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel GC: %v", err)
	}

	if err := s.CollectGarbage(t.Context()); err != nil {
		t.Fatal(err)
	}

	checkSemanticRows(t, s, "orphan", 0)
	checkSemanticRows(t, s, "live", 1)
	checkSemanticRows(t, s, "pending", 1)

	if err := s.RepairSemanticOrphans(t.Context()); err != nil {
		t.Fatal(err)
	}

	checkSemanticRows(t, s, "active", 1)
	checkSemanticRows(t, s, "pending", 0)
	checkSemanticRows(t, s, "live", 1)
}

func checkSemanticRows(t *testing.T, s *Store, build string, want int) {
	t.Helper()

	var n int
	if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build WHERE build_id=?", build).Scan(&n); err != nil || n != want {
		t.Fatalf("%s rows=%d want=%d err=%v", build, n, want, err)
	}
}

func stageNamedSemantic(t *testing.T, s *Store, name string) semantic.Generation {
	t.Helper()

	g, coverage, chunks := semanticFixture()
	g.BuildID = name

	chunks[0].BuildID = name
	if _, err := s.Stage(t.Context(), g, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	return g
}

// Removing CAS or retiring all nonactive builds loses the competing pending build.
func TestSemanticPublicationCASRetiresOnlyPrevious(t *testing.T) {
	s, _ := openTestStore(t, true)
	source, _, _ := semanticFixture()
	seedSemanticSource(t, s, source)
	first := stageNamedSemantic(t, s, "first")
	second := stageNamedSemantic(t, s, "second")

	pending := stageNamedSemantic(t, s, "pending")
	other, coverage, chunks := semanticFixture()
	other.BuildID = "other-kind"
	other.Scope.Kind = memory.ScopeKindGroup
	// Use the same valid key across different kinds.
	other.Scope.Key = first.Scope.Key
	chunks[0].BuildID = other.BuildID
	chunks[0].Scope = other.Scope
	seedSemanticSource(t, s, other)

	if _, err := s.Stage(t.Context(), other, coverage, chunks); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: other.View(), BuildID: other.BuildID}); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: first.View(), BuildID: first.BuildID}); err != nil {
		t.Fatal(err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: second.View(), BuildID: second.BuildID}); !errors.Is(err, semantic.ErrConflict) {
		t.Fatalf("stale absent CAS: %v", err)
	}

	snapshot, err := s.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	if _, found, err := activeSemanticState(snapshot.ctx, snapshot.tx, first.Scope); err != nil || !found {
		t.Fatalf("pin: %v %v", found, err)
	}

	if err := s.Publish(t.Context(), semantic.Publication{View: second.View(), BuildID: second.BuildID, ExpectedActiveBuildID: first.BuildID, ExpectedActiveEpoch: 1}); err != nil {
		t.Fatal(err)
	}

	state, found, err := s.ActiveState(t.Context(), first.Scope)
	if err != nil || !found || state.BuildID != second.BuildID || state.PublicationEpoch != 2 {
		t.Fatalf("active: %#v %v %v", state, found, err)
	}

	for _, test := range []struct {
		build string
		count int
	}{{first.BuildID, 0}, {second.BuildID, 1}, {pending.BuildID, 1}, {other.BuildID, 1}} {
		var n int
		if err := s.reader.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build WHERE build_id=?", test.build).Scan(&n); err != nil || n != test.count {
			t.Fatalf("%s count=%d err=%v", test.build, n, err)
		}
	}

	var old int
	if err := snapshot.tx.QueryRowContext(snapshot.ctx, "SELECT count(*) FROM semantic_chunk WHERE build_id=?", first.BuildID).Scan(&old); err != nil || old != 1 {
		t.Fatalf("MVCC old=%d err=%v", old, err)
	}
}
