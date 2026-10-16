//nolint:lll // Keep corruption SQL and expected errors next to each other.
package sqlite

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	semantic "github.com/baldaworks/boyan/internal/semantic"
	"github.com/baldaworks/boyan/pkg/memory"
	sqlitedriver "modernc.org/sqlite"
)

// Every mutation bypasses a different portable metadata/coverage gate. Request
// exclusion must not conceal corruption in the selected complete generation.
func TestSearchSnapshotRejectsPortableMetadataCorruption(t *testing.T) {
	for _, tc := range []struct {
		name, query   string
		unconstrained bool
	}{
		{"kind", "UPDATE semantic_chunk SET kind='invalid-kind'", false},
		{"input hash", "UPDATE semantic_chunk SET input_hash='invalid-hash'", false},
		{"zero sequence", "UPDATE semantic_chunk SET change_seq=zeroblob(8)", false},
		{"future sequence", "UPDATE semantic_chunk SET change_seq=x'0000000000000002'", false},
		{"source nul", "INSERT INTO semantic_source(chunk_rowid,id) SELECT rowid,'invalid'||char(0) FROM semantic_chunk", false},
		{"conversation nul", "INSERT INTO semantic_conversation(chunk_rowid,id) SELECT rowid,'invalid'||char(0) FROM semantic_chunk", false},
		{"item binding", "UPDATE semantic_chunk SET item_id='alien-item'", false},
		{"revision binding", "UPDATE semantic_chunk SET revision=x'0000000000000002'", false},
		{"content binding", "UPDATE semantic_chunk SET content_hash=replace(content_hash,'b','d')", false},
		{"revision identity", "UPDATE semantic_chunk SET revision_id='alien-revision'", false},
		{"category nul", "UPDATE semantic_chunk SET category=char(0)||'invalid'", false},
		{"memory key nul", "UPDATE semantic_chunk SET memory_key=char(0)||'invalid'", false},
		{"sibling metadata", "UPDATE semantic_chunk SET category='fact' WHERE ordinal=1", false},
		{"duplicate ordinal", "UPDATE semantic_chunk SET ordinal=0 WHERE ordinal=1", true},
		{"encoded bytes", "UPDATE semantic_build SET encoded_bytes=encoded_bytes+1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			second := c[0]
			second.Ordinal = 1
			c = append(c, second)

			r[0].ChunkCount = 2
			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			execTest(t, s.writer, "PRAGMA foreign_keys=OFF")

			if tc.unconstrained {
				execTest(t, s.writer, "ALTER TABLE semantic_chunk RENAME TO corrupt_original")
				execTest(t, s.writer, "CREATE TABLE semantic_chunk AS SELECT rowid,* FROM corrupt_original")
			}

			execTest(t, s.writer, tc.query)

			snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()

			for _, source := range []string{"", "excluded"} {
				request := memory.RecallRequest{Scope: g.Scope, Query: "query", SourceID: source, Limit: 1, AsOf: &g.ReadyAt}
				if hits, _, err := snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1); !errors.Is(err, semantic.ErrCorrupt) {
					t.Errorf("source=%q hits=%#v corruption=%v", source, hits, err)
				}
			}
		})
	}
}

// A driver callback is an execution barrier: cancellation occurs while SQLite
// evaluates the selected metadata scan, rather than before entry or via a timer.
func TestSearchSnapshotInFlightScanCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	name := fmt.Sprintf("cancel_search_metadata_%d", semanticCancelSequence.Add(1))

	var calls int

	if err := sqlitedriver.RegisterScalarFunction(name, 1, func(_ *sqlitedriver.FunctionContext, args []driver.Value) (driver.Value, error) {
		calls++

		cancel()

		return args[0], nil
	}); err != nil {
		t.Fatal(err)
	}

	s, _ := openTestStore(t, true)
	g, r, c := semanticFixture()
	seedSemanticSource(t, s, g)

	if _, err := s.Stage(t.Context(), g, r, c); err != nil {
		t.Fatal(err)
	}

	activateSemanticFixture(t, s, g)
	execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
	execTest(t, s.writer, "ALTER TABLE semantic_chunk RENAME TO stored_chunks")
	execTest(t, s.writer, `CREATE VIEW semantic_chunk AS SELECT rowid,build_id,item_id,revision_id,revision,ordinal,
 content_hash,input_hash,change_seq,dimensions,embedding,kind,category,memory_key,created_at,`+name+`(observed_at) AS observed_at,
 valid_from,valid_until,expires_at FROM stored_chunks`)

	snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()

	request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
	if _, _, err = snapshot.SearchSemantic(ctx, request, [][]float32{{1, 0}}, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan cancellation=%v calls=%d", err, calls)
	}

	if calls == 0 {
		t.Fatal("cancellation never reached an in-flight SQL scan")
	}

	if err = snapshot.Close(); err != nil {
		t.Fatal(err)
	}

	if got := s.reader.Stats().InUse; got != 0 {
		t.Fatalf("leaked readers=%d", got)
	}
}

// The pool barrier deterministically orders capture before the cutoff inserted
// while Open is queued. No sleep or wall-clock-expiry wait drives this assertion.
//
//nolint:cyclop,funlen // Keep pool ownership and both captured/explicit temporal assertions together.
func TestSearchSnapshotCapturesTimeBeforePoolWait(t *testing.T) {
	s, _ := openTestStore(t, false)

	first, err := s.OpenSearchSnapshot(t.Context(), lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := s.OpenSearchSnapshot(t.Context(), lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	type opened struct {
		snapshot semantic.SearchSnapshot
		err      error
	}

	result := make(chan opened, 1)

	before := s.reader.Stats().WaitCount
	captured := time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)

	var clockAdvance atomic.Int64

	go func() {
		snapshot, err := s.openSearchSnapshot(t.Context(), lexicalTestScope, func() time.Time {
			return captured.Add(time.Duration(clockAdvance.Load()) * time.Second)
		})
		result <- opened{snapshot, err}
	}()

	deadline := time.Now().Add(time.Second)
	for s.reader.Stats().WaitCount == before {
		if time.Now().After(deadline) {
			t.Fatal("snapshot did not queue behind the read pool")
		}

		runtime.Gosched()
	}

	clockAdvance.Store(2)

	cutoff := captured.Add(time.Second)
	past := captured.Add(-time.Second)
	seedLexical(t, s, "captured", lexicalTestScope, []lexicalFixture{{id: "eligible", raw: "needle", observed: &past, expiry: &cutoff}})
	execTest(t, s.writer, "UPDATE lexical_build SET record_count=1 WHERE physical_build_id='captured'")

	if err = first.Close(); err != nil {
		t.Fatal(err)
	}

	view := <-result
	if view.err != nil {
		t.Fatal(view.err)
	}
	defer view.snapshot.Close()

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1}

	hits, err := view.snapshot.SearchLexical(t.Context(), request, 1)
	if err != nil || len(hits) != 1 {
		t.Fatalf("captured time hits=%#v err=%v", hits, err)
	}

	request.AsOf = &cutoff

	hits, err = view.snapshot.SearchLexical(t.Context(), request, 1)
	if err != nil || len(hits) != 0 {
		t.Fatalf("explicit exclusive cutoff hits=%#v err=%v", hits, err)
	}
}

// The ordinal callback runs only if the metadata SELECT materializes a row.
// A corrupt large fixed-width field must be rejected by SQL preflight first;
// allocation counts are diagnostic and do not impose a flaky heap threshold.
func TestSearchSnapshotLargeCorruptMetadataPreflight(t *testing.T) {
	for _, field := range []string{"revision", "change_seq", "created_at", "observed_at", "valid_from", "valid_until", "expires_at"} {
		t.Run(field, func(t *testing.T) {
			var materialized int

			name := fmt.Sprintf("metadata_materialized_%d", semanticCancelSequence.Add(1))
			if err := sqlitedriver.RegisterScalarFunction(name, 1, func(_ *sqlitedriver.FunctionContext, args []driver.Value) (driver.Value, error) {
				materialized++

				return args[0], nil
			}); err != nil {
				t.Fatal(err)
			}

			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			execTest(t, s.writer, "PRAGMA ignore_check_constraints=ON")
			execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
			// field is selected only from the fixed internal test list above.
			execTest(t, s.writer, "UPDATE semantic_chunk SET "+field+"=zeroblob(8388608)")
			execTest(t, s.writer, "ALTER TABLE semantic_chunk RENAME TO stored_chunks")
			execTest(t, s.writer, `CREATE VIEW semantic_chunk AS SELECT rowid,build_id,item_id,revision_id,revision,`+name+`(ordinal) AS ordinal,
 content_hash,input_hash,change_seq,dimensions,embedding,kind,category,memory_key,created_at,observed_at,
 valid_from,valid_until,expires_at FROM stored_chunks`)

			snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
			_, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1)

			runtime.ReadMemStats(&after)
			t.Logf("Go allocated bytes=%d", after.TotalAlloc-before.TotalAlloc)

			if !errors.Is(err, semantic.ErrCorrupt) || materialized != 0 {
				t.Fatalf("corruption=%v metadata rows materialized=%d", err, materialized)
			}
		})
	}
}

func TestSearchSnapshotLargeCorruptControlMetadata(t *testing.T) {
	for _, tc := range []struct{ name, table, field string }{
		{"state watermark", "semantic_build", "watermark"},
		{"state epoch", "semantic_build", "epoch"},
		{"state ready time", "semantic_build", "ready_at"},
		{"active epoch", "semantic_active", "epoch"},
		{"coverage revision", "semantic_coverage", "revision"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := openTestStore(t, true)
			g, r, c := semanticFixture()
			seedSemanticSource(t, s, g)

			if _, err := s.Stage(t.Context(), g, r, c); err != nil {
				t.Fatal(err)
			}

			activateSemanticFixture(t, s, g)
			execTest(t, s.writer, "PRAGMA ignore_check_constraints=ON")
			execTest(t, s.writer, "PRAGMA foreign_keys=OFF")
			execTest(t, s.writer, "UPDATE "+tc.table+" SET "+tc.field+"=zeroblob(8388608)")

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			snapshot, err := s.OpenSearchSnapshot(t.Context(), g.Scope)
			if err != nil {
				t.Fatal(err)
			}
			defer snapshot.Close()

			if tc.table == "semantic_coverage" {
				request := memory.RecallRequest{Scope: g.Scope, Query: "query", Limit: 1, AsOf: &g.ReadyAt}
				_, _, err = snapshot.SearchSemantic(t.Context(), request, [][]float32{{1, 0}}, 1)
			} else {
				_, _, err = snapshot.SemanticState()
			}

			runtime.ReadMemStats(&after)
			t.Logf("Go allocated bytes=%d", after.TotalAlloc-before.TotalAlloc)

			if !errors.Is(err, semantic.ErrCorrupt) {
				t.Fatalf("control corruption=%v", err)
			}
		})
	}
}

func TestSearchSnapshotLargeCorruptLexicalFixedFields(t *testing.T) {
	for _, tc := range []struct{ table, field string }{
		{"lexical_build", "watermark"}, {"lexical_record", "revision"}, {"lexical_record", "change_seq"},
	} {
		t.Run(tc.table+"/"+tc.field, func(t *testing.T) {
			s, _ := openTestStore(t, false)
			seedLexical(t, s, "lexical", lexicalTestScope, []lexicalFixture{{id: "revision", raw: "needle", observed: &lexicalTestTime}})
			execTest(t, s.writer, "UPDATE lexical_build SET record_count=1")
			execTest(t, s.writer, "PRAGMA ignore_check_constraints=ON")
			execTest(t, s.writer, "UPDATE "+tc.table+" SET "+tc.field+"=zeroblob(8388608)")

			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)

			snapshot, err := s.OpenSearchSnapshot(t.Context(), lexicalTestScope)
			if err == nil {
				defer snapshot.Close()

				request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}
				_, err = snapshot.SearchLexical(t.Context(), request, 1)
			}

			runtime.ReadMemStats(&after)
			t.Logf("Go allocated bytes=%d", after.TotalAlloc-before.TotalAlloc)

			var classified *memory.Error
			if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure {
				t.Fatalf("lexical corruption=%v", err)
			}
		})
	}
}
