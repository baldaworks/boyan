//nolint:cyclop,lll // Explicit SQL fixtures test scrub's scope, certificate and paging contracts.
package sqlite

import (
	"context"
	"database/sql"
	sqldriver "database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestCanonicalScrubValidatesWholeInputBeforeRemoval(t *testing.T) {
	store, _ := openTestStore(t, false)
	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "first", raw: "needle", sources: []string{"source"}}})

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{"source", "internal\tcontrol"}, nil); err == nil {
		t.Fatal("invalid later input accepted")
	}

	var count int
	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("validation was partial: %d %v", count, err)
	}
}

//nolint:funlen // Two finite pages verify atomic cancellation and retained source proof for retry.
func TestCanonicalScrubCanceledSemanticPageRetainsSourceProofForRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	calls := 0

	name := fmt.Sprintf("cancel_zero_coverage_%d", cancellationFunctionSequence.Add(1))
	if err := driver.RegisterScalarFunction(name, 0, func(*driver.FunctionContext, []sqldriver.Value) (sqldriver.Value, error) {
		calls++
		if calls == 129 {
			cancel()
		}

		return int64(0), nil
	}); err != nil {
		t.Fatal(err)
	}

	store, _ := openTestStore(t, true)

	var fixtures []lexicalFixture
	for i := range 129 {
		fixtures = append(fixtures, lexicalFixture{id: fmt.Sprintf("revision-%03d", i), raw: "needle", sources: []string{"matching-source"}})
	}

	seedLexical(t, store, "proof", lexicalTestScope, fixtures)
	seedZeroCoverage(t, store, "semantic-zero", lexicalTestScope, fixtures[0].id)

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		for _, fixture := range fixtures[1:] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_coverage VALUES('semantic-zero',?,?,?,'content',0)`, fixture.id, fixture.id, encodeUint64(1)); err != nil {
				return err
			}
		}

		_, err := tx.ExecContext(ctx, `CREATE TRIGGER cancel_zero AFTER DELETE ON semantic_coverage BEGIN SELECT `+name+`(); END`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(ctx, lexicalTestScope, []string{"matching-source"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation missing", err)
	}

	var coverage, proof, complete int
	if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage`).Scan(&coverage); err != nil || coverage != 1 {
		t.Fatalf("semantic current page did not roll back: %d %v", coverage, err)
	}

	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&proof); err != nil || proof != 129 {
		t.Fatalf("source proof removed before semantic completion: %d %v", proof, err)
	}

	if err = store.reader.QueryRow(`SELECT complete FROM semantic_build WHERE build_id='semantic-zero'`).Scan(&complete); err != nil || complete != 0 {
		t.Fatalf("partial cleanup retained certificate: %d %v", complete, err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{"matching-source"}, nil); err != nil {
		t.Fatal(err)
	}

	if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage`).Scan(&coverage); err != nil || coverage != 0 {
		t.Fatalf("retry lost semantic association: %d %v", coverage, err)
	}

	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&proof); err != nil || proof != 0 {
		t.Fatalf("successful retry left lexical proof: %d %v", proof, err)
	}
}

//nolint:funlen // One source scrub exercises large filters, pinned readers and unrelated pending semantic work.
func TestCanonicalScrubOversizedSourcePreservesPendingAndPinnedReader(t *testing.T) {
	store, _ := openTestStore(t, true)
	source := strings.Repeat("source", 400000)
	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "forgotten", raw: "needle", sources: []string{source}}})
	seedZeroCoverage(t, store, "pending", lexicalTestScope, "safe")

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE semantic_build SET complete=0,status='building' WHERE build_id='pending'`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `UPDATE semantic_coverage SET chunk_count=1 WHERE build_id='pending'`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_chunk(build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,dimensions,embedding,kind,created_at,observed_at)
 VALUES('pending','safe','safe',?,0,'content','input',?,1,?,'state',?,?)`, encodeUint64(1), encodeUint64(1), []byte{0, 0, 128, 63}, encodeTime(lexicalTestTime), encodeTime(lexicalTestTime)); err != nil {
			return err
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO semantic_source SELECT rowid,'safe-source' FROM semantic_chunk`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = snapshot.Close() }()

	build, err := snapshot.activeLexicalBuild(lexicalTestScope)
	if err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{source}, nil); err != nil {
		t.Fatal(err)
	}

	var count int
	for _, table := range []string{"semantic_chunk", "semantic_coverage", "semantic_source"} {
		if err = store.reader.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("pending %s removed: %d %v", table, count, err)
		}
	}

	var status, state string
	if err = store.reader.QueryRow(`SELECT status FROM semantic_build WHERE build_id='pending'`).Scan(&status); err != nil || status != "building" {
		t.Fatalf("unrelated pending certificate changed: %s %v", status, err)
	}

	if err = store.reader.QueryRow(`SELECT state FROM lexical_build WHERE physical_build_id='active'`).Scan(&state); err != nil || state != "building" {
		t.Fatalf("touched certificate not revoked: %s %v", state, err)
	}

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime}

	hits, err := snapshot.searchLexical(request, lexicalTestTime, build, 4)
	if err != nil || len(hits) != 1 {
		t.Fatalf("pinned reader lost coherence: %+v %v", hits, err)
	}
	// SQL candidates from the old snapshot still require canonical hydration.
	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_fts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("FTS material survived: %d %v", count, err)
	}
}

func TestCanonicalScrubPagesFilterIdentities(t *testing.T) {
	store, _ := openTestStore(t, false)

	var (
		fixtures []lexicalFixture
		ids      []string
	)

	for i := range 300 {
		id := fmt.Sprintf("revision-%03d", i)
		fixtures = append(fixtures, lexicalFixture{id: id, raw: "needle"})
		ids = append(ids, id)
	}

	seedLexical(t, store, "active", lexicalTestScope, fixtures)

	conns := make([]*sql.Conn, 0, readerConnections)
	for range readerConnections {
		conn, err := store.reader.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		conns = append(conns, conn)
		if _, err = driver.Limit(conn, sqlite3.SQLITE_LIMIT_VARIABLE_NUMBER, 200); err != nil {
			t.Fatal(err)
		}
	}

	for _, conn := range conns {
		_ = conn.Close()
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, nil, ids); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("paged scrub incomplete: %d %v", count, err)
	}
}

func TestCanonicalScrubZeroChunkCoverageAndState(t *testing.T) {
	for _, sourceOnly := range []bool{false, true} {
		store, _ := openTestStore(t, true)
		seedZeroCoverage(t, store, "local", lexicalTestScope, "revision")
		foreign := memory.Scope{Key: lexicalTestScope.Key, Kind: memory.ScopeKindPersonal}
		seedZeroCoverage(t, store, "foreign", foreign, "revision")
		seedLexical(t, store, "lexical-proof", lexicalTestScope, []lexicalFixture{{id: "revision", raw: "needle", sources: []string{"matching-source"}}})

		applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
		if err != nil {
			t.Fatal(err)
		}

		var sources, revisions []string
		if sourceOnly {
			sources = []string{"matching-source"}
		} else {
			revisions = []string{"revision"}
		}

		if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, sources, revisions); err != nil {
			t.Fatal(err)
		}

		var count, complete int
		if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage WHERE build_id='local'`).Scan(&count); err != nil || count != 0 {
			t.Fatalf("zerochunk survived: %d %v", count, err)
		}

		if err = store.reader.QueryRow(`SELECT complete FROM semantic_build WHERE build_id='local'`).Scan(&complete); err != nil || complete != 0 {
			t.Fatalf("certificate survived: %d %v", complete, err)
		}

		if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage WHERE build_id='foreign'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("foreign removed: %d %v", count, err)
		}
	}
}

func TestCanonicalScrubUnknownSourcePreservesPendingZeroAndEmptyBuilds(t *testing.T) {
	store, _ := openTestStore(t, true)
	seedZeroCoverage(t, store, "pending-zero", lexicalTestScope, "unrelated-revision")
	seedZeroCoverage(t, store, "pending-empty", lexicalTestScope, "empty-placeholder")

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE semantic_build SET complete=0,status='building'`); err != nil {
			return err
		}

		_, err := tx.ExecContext(ctx, `DELETE FROM semantic_coverage WHERE build_id='pending-empty'`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{"nonexistent-source"}, nil); err != nil {
		t.Fatal(err)
	}

	var coverage int
	if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage WHERE build_id='pending-zero'`).Scan(&coverage); err != nil || coverage != 1 {
		t.Fatalf("unrelated pending zero coverage changed: %d %v", coverage, err)
	}

	for _, build := range []string{"pending-zero", "pending-empty"} {
		var status, reason string
		if err = store.reader.QueryRow(`SELECT status,reason FROM semantic_build WHERE build_id=?`, build).Scan(&status, &reason); err != nil || status != "building" || reason != "" {
			t.Fatalf("unrelated %s state changed: %s %q %v", build, status, reason, err)
		}
	}
}

func TestCanonicalScrubZeroCoverageUsesSemanticSourceProofWithoutLexicalRows(t *testing.T) {
	store, _ := openTestStore(t, true)
	seedZeroCoverage(t, store, "pending-zero", lexicalTestScope, "revision")
	seedZeroCoverage(t, store, "source-proof", lexicalTestScope, "revision")

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE semantic_coverage SET chunk_count=1 WHERE build_id='source-proof'`); err != nil {
			return err
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO semantic_chunk(build_id,item_id,revision_id,revision,ordinal,content_hash,input_hash,change_seq,dimensions,embedding,kind,created_at,observed_at)
 VALUES('source-proof','revision','revision',?,0,'content','input',?,1,?,'state',?,?)`, encodeUint64(1), encodeUint64(1), []byte{0, 0, 128, 63}, encodeTime(lexicalTestTime), encodeTime(lexicalTestTime)); err != nil {
			return err
		}

		_, err := tx.ExecContext(ctx, `INSERT INTO semantic_source SELECT rowid,'matching-source' FROM semantic_chunk`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{"matching-source"}, nil); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = store.reader.QueryRow(`SELECT count(*) FROM semantic_coverage`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("source-proven coverage survived: %d %v", count, err)
	}
}

func TestCanonicalScrubWaitsForCompetingApplierBeforeRemoval(t *testing.T) {
	store, _ := openTestStore(t, false)
	loaded, release := make(chan struct{}), make(chan struct{})
	reader := &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}, beforeContext: func(context.Context) { close(loaded); <-release }}

	applier, err := NewSQLiteCanonicalApplier(store, reader)
	if err != nil {
		t.Fatal(err)
	}

	competing, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	applied, scrubbed := make(chan error, 1), make(chan error, 1)
	go func() {
		applied <- applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "r"}})
	}()

	<-loaded

	go func() { scrubbed <- competing.ScrubCanonicalForget(t.Context(), lexicalTestScope, nil, []string{"r"}) }()
	// The serialized scrub may finish only after the already-hydrating apply.
	// Force its SQL enumeration to finish if it is incorrectly unsynchronized.
	select {
	case err = <-scrubbed:
		close(release)
		<-applied
		t.Fatalf("scrub overtook in-flight record and could reinsert forgotten material: %v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(release)

	if err = <-applied; err != nil {
		t.Fatal(err)
	}

	if err = <-scrubbed; err != nil {
		t.Fatal(err)
	}

	var count int
	if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("late reinsert: %d %v", count, err)
	}
}

func seedZeroCoverage(t *testing.T, store *Store, build string, scope memory.Scope, revision string) {
	t.Helper()

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO semantic_build(build_id,scope_kind,scope_key,generation_id,physical_build_id,watermark,model_space,dimensions,epoch,complete,status)
 VALUES(?,?,?,'generation','physical',?,'model',1,?,1,'ready')`, build, scope.Kind, scope.Key, encodeUint64(1), encodeUint64(1))
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO semantic_coverage VALUES(?,?,?,?,'content',0)`, build, revision, revision, encodeUint64(1))

		return err
	}); err != nil {
		t.Fatal(err)
	}
}
