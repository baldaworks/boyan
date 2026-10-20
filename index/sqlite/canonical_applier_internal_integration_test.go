//go:build integration

//nolint:cyclop,lll,funlen // Real SQL lifecycle scenarios retain explicit error checks and fixture shapes.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/pkg/memory"
)

type applyReader struct {
	memory.RecallCanonicalReader

	records       map[string]memory.RecallRecord
	before        func()
	maxIDs        int
	beforeContext func(context.Context)
}

func TestCanonicalPageRollbackAndCanceledContinuation(t *testing.T) {
	store, _ := openTestStore(t, false)
	reader := &applyReader{records: map[string]memory.RecallRecord{}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	revisions := make([]memory.MemoryRevision, 130)
	for i := range revisions {
		id := fmt.Sprintf("r-%03d", i)
		reader.records[id] = applyRecord(id)
		revisions[i] = memory.MemoryRevision{RevisionID: id}
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	calls := 0
	reader.before = func() {
		calls++
		if calls == 130 {
			cancel()
		}
	}

	if err = applier.Apply(ctx, lexicalTestScope, "new", nil, revisions); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record").Scan(&count); err != nil || count != 128 {
		t.Fatalf("page boundary: %d %v", count, err)
	}

	reader.before = nil
	// A SQL failure after an earlier row in the page must roll that row back.
	if err = store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER fail_page BEFORE INSERT ON lexical_record WHEN NEW.revision_id='r-128' BEGIN SELECT RAISE(ABORT,'page failure'); END`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	reader.records["added"] = applyRecord("added")

	if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "added"}, {RevisionID: "r-128"}}); err == nil {
		t.Fatal("failed page committed")
	}

	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record WHERE revision_id='added'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial page: %d %v", count, err)
	}
}

func TestCanonicalApplyReplacesMembershipAndDeletesMissing(t *testing.T) {
	store, _ := openTestStore(t, false)
	reader := &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	revisions := []memory.MemoryRevision{{RevisionID: "r"}}
	if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, revisions); err != nil {
		t.Fatal(err)
	}

	r := reader.records["r"]
	r.SourceIDs = []string{"replacement"}
	r.ConversationIDs = []string{"new-conversation"}
	reader.records["r"] = r

	if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, revisions); err != nil {
		t.Fatal(err)
	}
	// Test-only sealing permits the real query to inspect candidate membership.
	if err = store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE lexical_build SET state='committed'")

		return err
	}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := store.read(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 1, SourceID: "source"}

	hits, err := snapshot.searchLexical(request, lexicalTestTime, applier.stages[lexicalTestScope].id, 4)
	if err != nil || len(hits) != 0 {
		t.Fatalf("obsolete source retained: %v %v", hits, err)
	}

	request.SourceID = "replacement"
	request.ConversationID = "new-conversation"
	hits, err = snapshot.searchLexical(request, lexicalTestTime, applier.stages[lexicalTestScope].id, 4)
	_ = snapshot.Close()

	if err != nil || len(hits) != 1 {
		t.Fatalf("new memberships absent: %v %v", hits, err)
	}

	delete(reader.records, "r")

	if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, revisions); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"lexical_record", "lexical_fts", "lexical_source", "lexical_conversation"} {
		var count int
		if err = store.reader.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s survived deletion: %d %v", table, count, err)
		}
	}
}

func TestCanonicalApplyLargeSplitAndCanonicalOutsideWriter(t *testing.T) {
	store, _ := openTestStore(t, false)
	reader := &applyReader{records: map[string]memory.RecallRecord{}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	reader.before = func() {
		if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "SELECT 1")

			return err
		}); err != nil {
			t.Fatal(err)
		}
	}

	revisions := make([]memory.MemoryRevision, 8193)
	for i := range revisions {
		id := fmt.Sprintf("large-%05d", i)

		r := applyRecord(id)
		if i < 128 {
			r.Text = strings.Repeat("needle ", memory.MaxDerivedTextBytes/7)
		}

		reader.records[id] = r
		revisions[i] = memory.MemoryRevision{RevisionID: id, ItemID: id, Revision: 1}
	}

	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "large"); err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), lexicalTestScope, "large", nil, revisions); err != nil {
		t.Fatal(err)
	}

	var records, fts int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record").Scan(&records); err != nil {
		t.Fatal(err)
	}

	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_fts").Scan(&fts); err != nil {
		t.Fatal(err)
	}

	if records != 8193 || fts != 8193 {
		t.Fatalf("truncated/FTS mismatch: %d %d", records, fts)
	}
}

func TestCanonicalScrubExactScopeAndIdentities(t *testing.T) {
	store, _ := openTestStore(t, true)
	seedLexical(t, store, "a", lexicalTestScope, []lexicalFixture{{id: "forgotten", raw: "needle", sources: []string{"source"}}, {id: "safe", raw: "needle", sources: []string{"safe"}}})
	foreign := memory.Scope{Kind: memory.ScopeKindPersonal, Key: lexicalTestScope.Key}
	seedLexical(t, store, "b", foreign, []lexicalFixture{{id: "forgotten", raw: "needle", sources: []string{"source"}}})

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{}, fixtureCanonicalPrivacy(t, store, &applyReader{}))
	if err != nil {
		t.Fatal(err)
	}
	// Semantic staging is a later task; create a persisted obsolete fixture only.
	if err = store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO semantic_build(build_id,scope_kind,scope_key,generation_id,
 physical_build_id,watermark,model_space,dimensions,epoch,complete,status)
 VALUES('semantic',?,?,'a','a',?,'model',1,?,1,'ready')`, lexicalTestScope.Kind, lexicalTestScope.Key, encodeUint64(1), encodeUint64(1))
		if err != nil {
			return err
		}

		if _, err = tx.ExecContext(ctx, `INSERT INTO semantic_coverage VALUES('semantic','forgotten','forgotten',?,'hash',1)`, encodeUint64(1)); err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO semantic_chunk(build_id,item_id,revision_id,revision,ordinal,
 content_hash,input_hash,change_seq,dimensions,embedding,kind,created_at,observed_at)
 VALUES('semantic','forgotten','forgotten',?,0,'hash','input',?,1,?,'state',?,?)`, encodeUint64(1), encodeUint64(1), []byte{0, 0, 128, 63}, encodeTime(lexicalTestTime), encodeTime(lexicalTestTime))
		if err != nil {
			return err
		}

		_, err = tx.ExecContext(ctx, `INSERT INTO semantic_source SELECT rowid,'source' FROM semantic_chunk`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{" source"}, nil); err == nil {
		t.Fatal("forged identity accepted")
	}

	if err = applier.ScrubCanonicalForget(t.Context(), lexicalTestScope, []string{"source"}, nil); err != nil {
		t.Fatal(err)
	}

	var records, fts int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record").Scan(&records); err != nil {
		t.Fatal(err)
	}

	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_fts").Scan(&fts); err != nil {
		t.Fatal(err)
	}

	if records != 2 || fts != 2 {
		t.Fatalf("scope/FTS mismatch: %d %d", records, fts)
	}

	for _, table := range []string{"semantic_chunk", "semantic_source", "semantic_coverage"} {
		var count int
		if err = store.reader.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s survived scrub: %d %v", table, count, err)
		}
	}
}

func TestCanonicalRejectsStaleAndForeignRecords(t *testing.T) {
	for _, mutate := range []func(*memory.RecallRecord){func(r *memory.RecallRecord) { r.Scope.Kind = memory.ScopeKindPersonal }, func(r *memory.RecallRecord) { r.Revision = 2 }, func(r *memory.RecallRecord) { r.RevisionID = "unrequested" }, func(r *memory.RecallRecord) { r.State = memory.RevisionStateSuperseded }} {
		store, _ := openTestStore(t, false)
		record := applyRecord("revision")
		mutate(&record)
		reader := &applyReader{records: map[string]memory.RecallRecord{"revision": record}}

		applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
		if err != nil {
			t.Fatal(err)
		}

		if err = applier.Apply(t.Context(), lexicalTestScope, "new", nil, []memory.MemoryRevision{{RevisionID: "revision", ItemID: "revision", Revision: 1}}); err == nil {
			t.Fatal("invalid canonical record accepted")
		}
	}
}

func (r *applyReader) LoadRecallRecords(ctx context.Context, _ memory.Scope, ids []string) ([]memory.RecallRecord, error) {
	r.maxIDs = max(r.maxIDs, len(ids))
	if r.beforeContext != nil {
		r.beforeContext(ctx)
	}

	if r.before != nil {
		r.before()
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var records []memory.RecallRecord
	for _, id := range ids {
		if record, ok := r.records[id]; ok {
			records = append(records, record)
		}
	}

	return records, nil
}

func TestCanonicalApplyStagesAndCopies(t *testing.T) {
	store, _ := openTestStore(t, false)
	seedLexical(t, store, "old", lexicalTestScope, []lexicalFixture{{id: "old", raw: "needle", sources: []string{"old-source"}}})

	reader := &applyReader{records: map[string]memory.RecallRecord{"new": applyRecord("new")}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.Apply(t.Context(), lexicalTestScope, "old", nil, []memory.MemoryRevision{{RevisionID: "new", ItemID: "new", Revision: 1}}); err != nil {
		t.Fatal(err)
	}

	stage := applier.stages[lexicalTestScope]

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_record WHERE physical_build_id=?", stage.id).Scan(&count); err != nil || count != 2 {
		t.Fatalf("copy: %d %v", count, err)
	}

	hits, err := store.SearchRecall(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 10, AsOf: &lexicalTestTime})
	if err != nil || len(hits) != 1 || hits[0].RevisionID != "old" {
		t.Fatalf("active changed: %v %v", hits, err)
	}

	if stage.id == "old" {
		t.Fatal("mutated active physical identity")
	}

	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "other"); err == nil {
		t.Fatal("conflicting begin succeeded")
	}
}

func TestCanonicalRebuildOwnershipAndEmpty(t *testing.T) {
	store, _ := openTestStore(t, false)
	reader := &applyReader{}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil {
		t.Fatal(err)
	}

	id := applier.stages[lexicalTestScope].id
	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil || id != applier.stages[lexicalTestScope].id {
		t.Fatalf("non-idempotent: %v", err)
	}

	competing, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	if err = competing.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err == nil {
		t.Fatal("competing owner accepted")
	}

	if err = applier.EndProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err = store.reader.QueryRow("SELECT count(*) FROM lexical_active").Scan(&count); err != nil || count != 0 {
		t.Fatalf("empty published: %d %v", count, err)
	}
}

func TestCanonicalBatchRebuildUsesValidatedScopeAndCanonicalText(t *testing.T) {
	store, _ := openTestStore(t, false)
	record := applyRecord("batch")
	reader := &applyReader{records: map[string]memory.RecallRecord{"batch": record}}

	applier, err := NewSQLiteCanonicalApplier(store, reader, fixtureCanonicalPrivacy(t, store, reader))
	if err != nil {
		t.Fatal(err)
	}

	evidence, err := memory.NewEvidenceRef("source", "message", memory.MessageRoleUser, "x", 0, 1, memory.AssertionModeUser)
	if err != nil {
		t.Fatal(err)
	}

	revision := memory.MemoryRevision{SchemaVersion: memory.MemorySchemaVersion, RevisionID: "batch", ItemID: "batch", Revision: 1,
		Temporal: record.Temporal, Evidence: []memory.EvidenceRef{evidence}, Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard,
		Payload: memory.PayloadRef{ID: "payload", Digest: strings.Repeat("0", 64), ByteSize: 1}}

	view := memory.BoundedProjectionView{SchemaVersion: memory.ProjectionViewSchemaVersion,
		State:  memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: lexicalTestScope, Version: 1, ChangeSeq: 1},
		Active: []memory.ActiveProjectionRevision{{Item: memory.MemoryItem{ItemID: "batch", Scope: lexicalTestScope, Kind: memory.MemoryKindState, MemoryKey: "key"}, Revision: revision}}}
	if err = applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "batch-generation"); err != nil {
		t.Fatal(err)
	}

	if err = applier.ApplyProjectionRebuildBatch(t.Context(), lexicalTestScope, "batch-generation", view); err != nil {
		t.Fatal(err)
	}

	if err = applier.EndProjectionRebuild(t.Context(), lexicalTestScope, "batch-generation"); err != nil {
		t.Fatal(err)
	}

	var tokens string
	if err = store.reader.QueryRow(`SELECT raw_tokens FROM lexical_record`).Scan(&tokens); err != nil || tokens != "searchable needle" {
		t.Fatalf("canonical text missing: %q %v", tokens, err)
	}

	foreign := lexicalTestScope

	foreign.Kind = memory.ScopeKindPersonal
	if err = applier.ApplyProjectionRebuildBatch(t.Context(), foreign, "batch-generation", view); err == nil {
		t.Fatal("foreign view accepted")
	}
}
