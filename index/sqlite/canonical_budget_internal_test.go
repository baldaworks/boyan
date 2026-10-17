//nolint:cyclop,lll // Real transaction boundary fixtures retain explicit checks.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
	driver "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

func TestCanonicalOversizedRecordIsIsolatedAndHydratedAlone(t *testing.T) {
	store, _ := openTestStore(t, false)

	reader := &applyReader{records: map[string]memory.RecallRecord{}}
	for _, id := range []string{"before", "oversized", "tail"} {
		reader.records[id] = applyRecord(id)
	}

	large := reader.records["oversized"]
	large.ConversationIDs = []string{strings.Repeat("c", lexicalPageBytes+1)}
	reader.records["oversized"] = large

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	failCanonicalInsert(t, store, "tail")

	err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "before"}, {RevisionID: "oversized"}, {RevisionID: "tail"}})
	if err == nil {
		t.Fatal("tail failure missing")
	}

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record").Scan(&count); err != nil || count != 2 {
		t.Fatalf("isolated large record lost: count=%d err=%v", count, err)
	}

	if reader.maxIDs != 1 {
		t.Fatalf("hydrated %d identities at once", reader.maxIDs)
	}

	var conversation string
	if err = store.reader.QueryRow("SELECT id FROM lexical_conversation WHERE length(id)>2000000").Scan(&conversation); err != nil || conversation != large.ConversationIDs[0] {
		t.Fatal("large exact membership changed", err)
	}
}

func TestCanonicalCloseCancelsHydrationAndDrains(t *testing.T) {
	store, _ := openTestStore(t, false)
	started := make(chan struct{})
	reader := &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}, beforeContext: func(ctx context.Context) { close(started); <-ctx.Done() }}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	result := make(chan error, 1)
	go func() {
		result <- applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "r"}})
	}()

	<-started

	deadline, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	if err = store.CloseContext(deadline); err != nil {
		t.Fatal("hydration was not canceled/drained", err)
	}

	if err = <-result; !errors.Is(err, context.Canceled) {
		t.Fatal("apply did not preserve cancellation", err)
	}

	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "new"); !errors.Is(err, ErrClosed) {
		t.Fatal("closed begin accepted", err)
	}
}

func TestCanonicalAccountingIncludesNormalizedDuplicatedTokensAndOverflow(t *testing.T) {
	r := applyRecord("r")

	input, err := canonicalLexicalInput(r)
	if err != nil {
		t.Fatal(err)
	}

	n, err := input.encodedBytes("b")
	// 35 identity/rowid binds + 105 scalar bytes + 32 repeated FTS token
	// bytes + 14 source membership bytes + 20 conversation membership bytes.
	if err != nil || n != 206 {
		t.Fatalf("ordinary encoded inputs: %d %v", n, err)
	}

	r.Text = "cafe\u0301 кошки running"

	input, err = canonicalLexicalInput(r)
	if err != nil {
		t.Fatal(err)
	}

	n, err = input.encodedBytes("b")
	// NFC raw=24 bytes, RU=8, EN=9, each sent twice; +142 other bytes.
	if err != nil || n != 224 {
		t.Fatalf("normalized encoded inputs: %d %v", n, err)
	}

	if _, err = checkedInputBytes(math.MaxUint64, 1); err == nil {
		t.Fatal("accounting wrapped")
	}

	if n, err = checkedInputBytes(math.MaxUint64-1, 1); err != nil || n != math.MaxUint64 {
		t.Fatal("at-limit accounting failed", n, err)
	}
}

func TestCanonicalBudgetTargetEdgesHaveRealTransactionBoundaries(t *testing.T) {
	for _, delta := range []int{-1, 0, 1} {
		t.Run(fmt.Sprint(delta), func(t *testing.T) {
			store, _ := openTestStore(t, false)
			record := applyRecord("edge")
			// Same fixture scalar binds as the accounting test, with build ID40 bytes
			// (two occurrences), and item/revision IDs4 bytes (three occurrences).
			// 206 + 78 + 9 =293. One membership adds its 8-byte rowid plus ID bytes.
			record.ConversationIDs = append(record.ConversationIDs, strings.Repeat("c", lexicalPageBytes+delta-293-8))
			reader := &applyReader{records: map[string]memory.RecallRecord{"edge": record, "tail": applyRecord("tail")}}

			applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
			if err != nil {
				t.Fatal(err)
			}

			failCanonicalInsert(t, store, "tail")

			if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "edge"}, {RevisionID: "tail"}}); err == nil {
				t.Fatal("tail unexpectedly committed")
			}

			var count int
			if err = store.reader.QueryRow(`SELECT count(*) FROM lexical_record WHERE revision_id='edge'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("boundary record not committed separately: %d %v", count, err)
			}
		})
	}
}

func TestCanonicalBeginSanitizesSQLFailure(t *testing.T) {
	store, _ := openTestStore(t, false)
	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER fail_build BEFORE INSERT ON lexical_build BEGIN SELECT RAISE(ABORT,'private-build-value'); END`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{}, fixtureCanonicalPrivacy(t, store, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}

	err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "new")

	var classified *memory.Error
	if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe begin error: %v", err)
	}
}

func TestCanonicalObservedConnectionLimitsAndSingleRecordCopy(t *testing.T) {
	store, _ := openTestStore(t, false)
	if len(store.connectionLimits) != 3 {
		t.Fatal("uninspected physical connections", store.connectionLimits)
	}

	for i, limit := range store.connectionLimits {
		if limit.length <= lexicalPageBytes+1 || limit.variables < 130 {
			t.Fatalf("unrepresentable required fixture on connection%d: %+v", i, limit)
		}

		t.Logf("physical connection%d LENGTH=%d VARIABLE_NUMBER=%d", i, limit.length, limit.variables)
	}

	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "first", raw: "needle"}, {id: "second", raw: "needle"}})

	rows, last, err := store.loadLexicalPage(t.Context(), "active", 0)
	if err != nil || len(rows) != 1 || rows[0].revision != "first" {
		t.Fatalf("copy materialized multiple records: %d %v", len(rows), err)
	}

	rows, _, err = store.loadLexicalPage(t.Context(), "active", last)
	if err != nil || len(rows) != 1 || rows[0].revision != "second" {
		t.Fatalf("copy keyset lost record: %d %v", len(rows), err)
	}
}

func TestCanonicalAggregateMembershipOversize(t *testing.T) {
	store, _ := openTestStore(t, false)

	record := applyRecord("aggregate")
	for i := range 35000 {
		record.ConversationIDs = append(record.ConversationIDs, fmt.Sprintf("%06d-%s", i, strings.Repeat("x", 64)))
	}
	// Duplicate memberships must not multiply rows or fail the transaction.
	record.ConversationIDs = append(record.ConversationIDs, record.ConversationIDs[1])
	reader := &applyReader{records: map[string]memory.RecallRecord{"aggregate": record}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "aggregate"}}); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_conversation").Scan(&count); err != nil || count != 35001 {
		t.Fatalf("memberships: %d %v", count, err)
	}
}

//nolint:funlen // Keep connection-limit restoration, transaction rollback and no-sealing evidence together.
func TestCanonicalUnrepresentableRecordRollsBackAndSanitizes(t *testing.T) {
	store, _ := openTestStore(t, false)
	seedLexical(t, store, "active", lexicalTestScope, []lexicalFixture{{id: "safe", raw: "needle"}})

	record := applyRecord("secret-revision")
	record.ConversationIDs = []string{strings.Repeat("private-conversation", 1000)}
	reader := &applyReader{records: map[string]memory.RecallRecord{record.RevisionID: record}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	conn, err := store.writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	old, err := driver.Limit(conn, sqlite3.SQLITE_LIMIT_LENGTH, 4096)
	if err != nil {
		t.Fatal(err)
	}

	_ = conn.Close()

	defer func() {
		c, e := store.writer.Conn(t.Context())
		if e == nil {
			_, _ = driver.Limit(c, sqlite3.SQLITE_LIMIT_LENGTH, old)
			_ = c.Close()
		}
	}()

	err = applier.Apply(t.Context(), lexicalTestScope, "active", nil, []memory.MemoryRevision{{RevisionID: record.RevisionID}})
	if err == nil || strings.Contains(err.Error(), "SQLITE") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
		t.Fatalf("unsafe or missing storage failure: %v", err)
	}

	var classified *memory.Error
	if !errors.As(err, &classified) || classified.Code != memory.CodeStoreFailure {
		t.Fatalf("classification: %v", err)
	}

	if err := applier.CommitProjectionManifest(t.Context(), publicationManifest("active", 1)); err == nil {
		t.Fatal("unrepresentable partial work acquired a complete certificate")
	}

	var complete int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=? AND state='committed'`,
		applier.stages[lexicalTestScope].id).Scan(&complete); err != nil || complete != 0 {
		t.Fatalf("failed representability sealed physical state: %d %v", complete, err)
	}

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record WHERE revision_id='secret-revision'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial record: %d %v", count, err)
	}

	hits, err := store.SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, AsOf: &lexicalTestTime})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != "safe" {
		t.Fatalf("active lost: %+v %v", hits, err)
	}
}

func failCanonicalInsert(t *testing.T, store *Store, id string) {
	t.Helper()

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER fail_insert BEFORE INSERT ON lexical_record WHEN NEW.revision_id='`+id+`' BEGIN SELECT RAISE(ABORT,'injected'); END`)

		return err
	}); err != nil {
		t.Fatal(err)
	}
}
