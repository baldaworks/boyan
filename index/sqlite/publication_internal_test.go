//nolint:cyclop,lll,gocognit,funlen // Fault-driven lifecycle scenarios retain explicit real SQL and boundary assertions.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

type manifestCommitter interface {
	CommitProjectionManifest(ctx context.Context, manifest memory.ProjectionManifest) error
}

type publicationApplier interface {
	manifestCommitter
	Activate(ctx context.Context, scope memory.Scope, generation string) error
	ResumeProjection(ctx context.Context, scope memory.Scope, generation string) (bool, error)
	ConfigureProjectionRetention(checkpoints memory.ProjectionCheckpointStore, projectionID string)
	ReconcileProjection(ctx context.Context) error
	AbortProjectionSync(ctx context.Context, generation string) error
}

// The canonical boundary is controlled so crash windows can be reproduced;
// all projection writes, queries, snapshots and reopen operations use SQLite.
type publicationCheckpoints struct {
	memory.ProjectionCheckpointStore

	active   memory.ProjectionManifest
	previous []memory.ProjectionManifest
	err      error
	floor    uint64
	onLoad   func()
}

type publicationPlainCheckpoints struct {
	memory.ProjectionCheckpointStore

	state *publicationCheckpoints
}

func (c *publicationPlainCheckpoints) LoadActiveProjectionManifest(ctx context.Context,
	scope memory.Scope, projectionID string) (memory.ProjectionManifest, bool, error) {
	return c.state.LoadActiveProjectionManifest(ctx, scope, projectionID)
}

func (c *publicationPlainCheckpoints) LoadProjectionManifest(_ context.Context,
	_ memory.Scope, _ string, generation string) (memory.ProjectionManifest, bool, error) {
	if c.state.active.GenerationID == generation {
		return c.state.active, true, c.state.err
	}

	for _, manifest := range c.state.previous {
		if manifest.GenerationID == generation {
			return manifest, true, c.state.err
		}
	}

	return memory.ProjectionManifest{}, false, c.state.err
}

func (c *publicationCheckpoints) LoadActiveProjectionManifest(context.Context, memory.Scope, string) (memory.ProjectionManifest, bool, error) {
	if c.onLoad != nil {
		c.onLoad()
	}

	return c.active, c.active.GenerationID != "", c.err
}

func (c *publicationCheckpoints) WalkProjectionManifests(_ context.Context, _ string, visit func(memory.ProjectionManifest, bool) error) error {
	if c.err != nil {
		return c.err
	}

	if c.active.GenerationID != "" {
		if err := visit(c.active, true); err != nil {
			return err
		}
	}

	for _, old := range c.previous {
		if err := visit(old, false); err != nil {
			return err
		}
	}

	return nil
}

func (c *publicationCheckpoints) LoadProjectionRetentionFloor(context.Context, memory.Scope, string) (uint64, error) {
	return c.floor, c.err
}

func publicationLifecycle(t *testing.T, store *Store, reader memory.RecallCanonicalReader, checkpoints *publicationCheckpoints) (*SQLiteCanonicalApplier, publicationApplier) {
	t.Helper()

	a, err := NewSQLiteCanonicalApplier(store, reader)
	if err != nil {
		t.Fatal(err)
	}

	lifecycle, ok := any(a).(publicationApplier)
	if !ok {
		t.Fatal("SQLite lifecycle capabilities are missing")
	}

	lifecycle.ConfigureProjectionRetention(checkpoints, "sqlite")

	return a, lifecycle
}

func publicationManifest(generation string, watermark uint64) memory.ProjectionManifest {
	return memory.ProjectionManifest{Scope: lexicalTestScope, ProjectionID: "sqlite", GenerationID: generation,
		Status: memory.ProjectionGenerationActive, Watermark: watermark, UpdatedAt: lexicalTestTime}
}

func TestPublicationRequiresCanonicalExactCompleteTuple(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	manifest := publicationManifest("g", 2)

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err == nil {
		t.Fatal("incomplete build activated")
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	for _, mutate := range []func(*memory.ProjectionManifest){
		func(m *memory.ProjectionManifest) { m.Watermark = 1 },
		func(m *memory.ProjectionManifest) { m.Scope.Kind = memory.ScopeKindPersonal },
		func(m *memory.ProjectionManifest) { m.ProjectionID = "bleve" },
		func(m *memory.ProjectionManifest) { m.GenerationID = "other" },
		func(m *memory.ProjectionManifest) { m.Status = memory.ProjectionGenerationDirty },
	} {
		c.active = manifest
		mutate(&c.active)

		if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err == nil {
			t.Fatal("mismatching canonical tuple activated")
		}
	}

	c.active = manifest

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	physical, ok := any(store).(memory.PhysicalProjectionViewReader)
	if !ok {
		t.Fatal("physical manifest reader missing")
	}

	got, found, err := physical.LoadActivePhysicalProjectionManifest(t.Context(), lexicalTestScope, "sqlite")
	if err != nil || !found || got != manifest {
		t.Fatalf("exact physical certificate: %+v %v %v", got, found, err)
	}

	certified, ok := any(store).(memory.CertifiedRecallProjection)
	if !ok {
		t.Fatal("certified search missing")
	}

	request := memory.RecallRequest{Scope: lexicalTestScope, Query: "absent", Limit: 1, MinScopeChangeSeq: 2, AsOf: &lexicalTestTime}

	result, err := certified.SearchRecallCertified(t.Context(), request, 10)
	if err != nil || !result.Certified || len(result.Hits) != 0 {
		t.Fatalf("empty result certification: %+v %v", result, err)
	}

	c.active.Status = memory.ProjectionGenerationDirty

	if _, err := certified.SearchRecallCertified(t.Context(), request, 10); err == nil {
		t.Fatal("dirty empty result certified")
	}
}

func TestPublicationCrashReopenAndRetentionFloor(t *testing.T) {
	store, dir := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("empty", 7)
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}
	// Canonical activation succeeded; SQL activation did not run before crash.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(t.Context(), dir, Options{})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = reopened.Close() })
	_, lifecycle = publicationLifecycle(t, reopened, &applyReader{}, c)

	resumed, err := lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "empty")
	if err != nil || !resumed {
		t.Fatalf("canonical-first recovery: %v %v", resumed, err)
	}

	c.floor = 8

	resumed, err = lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "empty")
	if err != nil || resumed {
		t.Fatalf("retention floor ignored: %v %v", resumed, err)
	}

	c.floor = 0
	c.active.Status = memory.ProjectionGenerationDirty

	resumed, err = lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "empty")
	if err != nil || resumed {
		t.Fatalf("dirty checkpoint resumed: %v %v", resumed, err)
	}

	c.active = publicationManifest("missing", 7)

	resumed, err = lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "missing")
	if err != nil || resumed {
		t.Fatalf("missing physical resumed: %v %v", resumed, err)
	}

	c.err = errors.New("canonical unavailable")
	if _, err := lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "missing"); err == nil {
		t.Fatal("canonical I/O masked as absent")
	}
}

func TestPublicationCanceledApplyAndScrubCannotSeal(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}
	reader := &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}
	a, lifecycle := publicationLifecycle(t, store, reader, c)
	ctx, cancel := context.WithCancel(t.Context())
	reader.before = cancel

	if err := a.Apply(ctx, lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	manifest := publicationManifest("g", 1)
	if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err == nil {
		t.Fatal("canceled batch sealed")
	}

	reader.before = nil

	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := a.ScrubCanonicalForget(t.Context(), lexicalTestScope, nil, []string{"r"}); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err == nil {
		t.Fatal("scrub-revoked target recertified")
	}

	c.active = manifest

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err == nil {
		t.Fatal("scrub-revoked build activated")
	}

	deadline, done := context.WithTimeout(t.Context(), time.Second)
	defer done()

	if err := lifecycle.AbortProjectionSync(deadline, "g"); err != nil {
		t.Fatal(err)
	}
}

// A durable batch alone must not certify an empty scope or infer its watermark.
func TestPublicationEmptyExactAnchor(t *testing.T) {
	store, _ := openTestStore(t, false)

	applier, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	committer, ok := any(applier).(manifestCommitter)
	if !ok {
		t.Fatal("SQLite applier lacks exact physical manifest sealing")
	}

	if err := applier.BeginProjectionRebuild(t.Context(), lexicalTestScope, "empty"); err != nil {
		t.Fatal(err)
	}

	manifest := memory.ProjectionManifest{Scope: lexicalTestScope, ProjectionID: "sqlite", GenerationID: "empty",
		Status: memory.ProjectionGenerationDirty, Watermark: 9223372036854775810, UpdatedAt: lexicalTestTime}
	if err := committer.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	var (
		watermark []byte
		count     int
		state     string
	)
	if err := store.reader.QueryRowContext(t.Context(), `SELECT watermark,record_count,state FROM lexical_build`).Scan(&watermark, &count, &state); err != nil {
		t.Fatal(err)
	}

	got, err := decodeUint64(watermark)
	if err != nil || got != 9223372036854775810 || count != 0 || state != "committed" {
		t.Fatalf("empty anchor inferred from rows: %d count=%d state=%s err=%v", got, count, state, err)
	}
}

func TestPublicationEmptySyncCopiesAndReleasesSameTuple(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("g", 1)
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	var old string
	if err := store.reader.QueryRow(`SELECT physical_build_id FROM lexical_active`).Scan(&old); err != nil {
		t.Fatal(err)
	}
	// An empty coordinator sync still supplies an exact target and must finish
	// copying the active build before sealing its replacement.
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	var active string
	if err := store.reader.QueryRow(`SELECT physical_build_id FROM lexical_active`).Scan(&active); err != nil {
		t.Fatal(err)
	}

	if active == old || len(a.stages) != 0 {
		t.Fatal("same-tuple publication retained old build or leaked staging ownership")
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_record WHERE physical_build_id=?`, active).Scan(&count); err != nil || count != 1 {
		t.Fatalf("empty sync copy incomplete: %d %v", count, err)
	}
}

func TestPublicationPinnedReaderSurvivesRetentionAndPendingOwner(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}
	reader := &applyReader{records: map[string]memory.RecallRecord{}}
	a, lifecycle := publicationLifecycle(t, store, reader, c)

	var (
		pinned *readSnapshot
		first  string
	)

	for i, generation := range []string{"first", "second", "third"} {
		reader.records[generation] = applyRecord(generation)
		if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, generation); err != nil {
			t.Fatal(err)
		}

		if err := a.Apply(t.Context(), lexicalTestScope, generation, nil, []memory.MemoryRevision{{RevisionID: generation}}); err != nil {
			t.Fatal(err)
		}

		if c.active.GenerationID != "" {
			old := c.active
			old.Status = memory.ProjectionGenerationSuperseded
			c.previous = append(c.previous, old)
		}

		c.active = publicationManifest(generation, uint64(i+1))

		c.active.UpdatedAt = lexicalTestTime.Add(time.Duration(i) * time.Second)
		if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
			t.Fatal(err)
		}

		if err := lifecycle.Activate(t.Context(), lexicalTestScope, generation); err != nil {
			t.Fatal(err)
		}

		if i == 0 {
			var err error

			pinned, err = store.read(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = pinned.Close() }()

			first, err = pinned.activeLexicalBuild(lexicalTestScope)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, first).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unretained orphan survived: %d %v", count, err)
	}

	hits, err := pinned.searchLexical(memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 10}, lexicalTestTime, first, 10)
	if err != nil || len(hits) != 1 || hits[0].RevisionID != "first" {
		t.Fatalf("pinned view lost after pruning: %+v %v", hits, err)
	}

	if err := pinned.Close(); err != nil {
		t.Fatal(err)
	}

	if err := a.Apply(t.Context(), lexicalTestScope, "third", nil, nil); err != nil {
		t.Fatal(err)
	}

	pending := a.stages[lexicalTestScope].id

	other, err := NewSQLiteCanonicalApplier(store, reader)
	if err != nil {
		t.Fatal(err)
	}

	other.ConfigureProjectionRetention(c, "sqlite")

	if err := other.ReconcileProjection(t.Context()); err != nil {
		t.Fatal(err)
	}

	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, pending).Scan(&count); err != nil || count != 1 {
		t.Fatalf("foreign pending owner collected: %d %v", count, err)
	}

	if err := other.AbortProjectionSync(t.Context(), "third"); err != nil {
		t.Fatal(err)
	}

	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, pending).Scan(&count); err != nil || count != 1 {
		t.Fatalf("foreign abort collected pending: %d %v", count, err)
	}
}

func TestPublicationFaultsAndConcurrentNewerCanonicalRows(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}
	record := applyRecord("r")
	record.ScopeChangeSeq = 99

	a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": record}}, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	if _, err := store.writer.Exec(`CREATE TRIGGER fail_seal BEFORE UPDATE OF state ON lexical_build WHEN NEW.state='committed' BEGIN SELECT RAISE(ABORT,'seal fault'); END`); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("g", 2)
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err == nil {
		t.Fatal("seal failure ignored")
	}

	if resumed, err := lifecycle.ResumeProjection(t.Context(), lexicalTestScope, "g"); err != nil || resumed {
		t.Fatalf("partial seal resumed: %v %v", resumed, err)
	}

	if _, err := store.writer.Exec(`DROP TRIGGER fail_seal`); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if _, err := store.writer.Exec(`CREATE TRIGGER fail_activate BEFORE INSERT ON lexical_active BEGIN SELECT RAISE(ABORT,'activate fault'); END`); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err == nil {
		t.Fatal("activation failure ignored")
	}

	if _, err := store.writer.Exec(`DROP TRIGGER fail_activate`); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.ReconcileProjection(t.Context()); err != nil {
		t.Fatal(err)
	}

	manifest, found, err := store.LoadActivePhysicalProjectionManifest(t.Context(), lexicalTestScope, "sqlite")
	if err != nil || !found || manifest.Watermark != 2 {
		t.Fatalf("row sequence replaced rebuild anchor: %+v %v %v", manifest, found, err)
	}

	if err := store.write(t.Context(), func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE lexical_build SET record_count=99 WHERE state='committed'`)

		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := store.LoadActivePhysicalProjectionManifest(t.Context(), lexicalTestScope, "sqlite"); err == nil {
		t.Fatal("corrupt durable complete count certified")
	}
}

func TestPublicationConsistentPreservesOrdinaryCandidateDepth(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}
	reader := &applyReader{records: map[string]memory.RecallRecord{}}

	revisions := make([]memory.MemoryRevision, 200)
	for i := range revisions {
		id := fmt.Sprintf("candidate-%03d", i)
		reader.records[id] = applyRecord(id)
		revisions[i].RevisionID = id
	}

	a, lifecycle := publicationLifecycle(t, store, reader, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, revisions); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("g", 1)
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		limit int
		want  int
	}{{1, 4}, {50, 200}} {
		request := memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: tc.limit, MinScopeChangeSeq: 1, AsOf: &lexicalTestTime}

		hits, err := store.SearchRecallConsistent(t.Context(), request)
		if err != nil || len(hits) != tc.want {
			t.Fatalf("candidate depth for limit%d: %d want%d err=%v", tc.limit, len(hits), tc.want, err)
		}
	}

	if _, err := store.SearchRecallCertified(t.Context(), memory.RecallRequest{Scope: lexicalTestScope, Query: "needle", Limit: 50}, 101); err == nil {
		t.Fatal("private hybrid channel exceeded100")
	}
}

func TestPublicationRetentionDoesNotAccumulateSameTupleCopies(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{active: publicationManifest("g", 1)}

	_, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	for range 4 {
		if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
			t.Fatal(err)
		}

		if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
			t.Fatal(err)
		}
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("same tuple retained more than active+previous: %d %v", count, err)
	}
}

func TestPublicationAbortCleanupIsPagedAndRetryable(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}
	reader := &applyReader{records: map[string]memory.RecallRecord{}}

	revisions := make([]memory.MemoryRevision, 260)
	for i := range revisions {
		id := fmt.Sprintf("r%03d", i)
		reader.records[id] = applyRecord(id)
		revisions[i].RevisionID = id
	}

	a, lifecycle := publicationLifecycle(t, store, reader, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "pending", nil, revisions); err != nil {
		t.Fatal(err)
	}

	if _, err := store.writer.Exec(`CREATE TRIGGER fail_cleanup BEFORE DELETE ON lexical_record WHEN OLD.revision_id='r128' BEGIN SELECT RAISE(ABORT,'cleanup fault'); END`); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.AbortProjectionSync(t.Context(), "pending"); err == nil {
		t.Fatal("cleanup fault ignored")
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_record`).Scan(&count); err != nil || count != 132 {
		t.Fatalf("cleanup was not finite pages: %d %v", count, err)
	}

	if len(a.stages) != 1 {
		t.Fatal("interrupted cleanup lost ownership")
	}

	if _, err := store.writer.Exec(`DROP TRIGGER fail_cleanup`); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.AbortProjectionSync(t.Context(), "pending"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"lexical_record", "lexical_fts", "lexical_source", "lexical_conversation", "lexical_build"} {
		if err := store.reader.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("abort left %s: %d %v", table, count, err)
		}
	}
}

func TestPublicationAbortPreservesCanonicalPreviousBeforeSQLSwitch(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "previous"); err != nil {
		t.Fatal(err)
	}

	manifest := publicationManifest("previous", 3)
	if err := lifecycle.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	manifest.Status = memory.ProjectionGenerationSuperseded
	c.previous = []memory.ProjectionManifest{manifest}
	c.active = publicationManifest("missing", 4)
	id := a.stages[lexicalTestScope].id

	if err := lifecycle.AbortProjectionSync(t.Context(), "previous"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("previous lost before SQL switch: %d %v", count, err)
	}

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "replacement"); err != nil {
		t.Fatal("retained previous owner prevents recovery", err)
	}
}

func TestPublicationAbortAfterCheckpointAdvanceReleasesOwner(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "interrupted"); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("interrupted", 7)

	c.active.Status = memory.ProjectionGenerationDirty
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	id := a.stages[lexicalTestScope].id
	// The checkpoint advance succeeded and canonical activation failed. Runtime
	// aborts this session and starts a fresh generation on the next dirty sync.
	if err := lifecycle.AbortProjectionSync(t.Context(), "interrupted"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("retained dirty checkpoint build lost: %d %v", count, err)
	}

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "replacement"); err != nil {
		t.Fatal("failed session still owns scope", err)
	}
}

func TestPublicationPhysicalReaderRechecksScrubRevocation(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	c.active = publicationManifest("g", 1)
	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	loads := 0
	c.onLoad = func() {
		loads++
		if loads == 2 {
			if err := a.ScrubCanonicalForget(t.Context(), lexicalTestScope, nil, []string{"r"}); err != nil {
				t.Fatal(err)
			}
		}
	}

	if _, found, err := store.LoadActivePhysicalProjectionManifest(t.Context(), lexicalTestScope, "sqlite"); err != nil || found {
		t.Fatalf("revoked snapshot escaped physical recheck: %v %v", found, err)
	}
}

func TestPublicationLegacyCommitPreservesLifecycleErrors(t *testing.T) {
	store, _ := openTestStore(t, false)

	a, err := NewSQLiteCanonicalApplier(store, &applyReader{})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := a.Commit(ctx, lexicalTestScope, "g"); !errors.Is(err, context.Canceled) {
		t.Fatalf("legacy commit lost caller cancellation: %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if err := a.Commit(t.Context(), lexicalTestScope, "g"); !errors.Is(err, ErrClosed) {
		t.Fatalf("legacy commit lost storage lifetime: %v", err)
	}
}

func TestPublicationForeignResumeCannotPublishWritableOwnedStage(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{active: publicationManifest("g", 1)}

	a, lifecycle := publicationLifecycle(t, store, &applyReader{records: map[string]memory.RecallRecord{"r": applyRecord("r")}}, c)
	if err := a.Apply(t.Context(), lexicalTestScope, "g", nil, []memory.MemoryRevision{{RevisionID: "r"}}); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	_, foreign := publicationLifecycle(t, store, &applyReader{}, c)
	if resumed, err := foreign.ResumeProjection(t.Context(), lexicalTestScope, "g"); err == nil || resumed {
		t.Fatalf("foreign owner published stillwritable stage: %v %v", resumed, err)
	}

	if err := foreign.ReconcileProjection(t.Context()); err != nil {
		t.Fatal("maintenance must preserve and skip a foreign live publication", err)
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_active`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("foreign resume changed active: %d %v", count, err)
	}

	if err := lifecycle.Activate(t.Context(), lexicalTestScope, "g"); err != nil {
		t.Fatal(err)
	}

	if len(a.stages) != 0 {
		t.Fatal("owner activation did not release writable stage")
	}
}

func TestPublicationAbortRetainsDirtyPortableCheckpointWithoutWalker(t *testing.T) {
	store, _ := openTestStore(t, false)
	c := &publicationCheckpoints{active: publicationManifest("interrupted", 7)}
	c.active.Status = memory.ProjectionGenerationDirty
	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	lifecycle.ConfigureProjectionRetention(&publicationPlainCheckpoints{state: c}, "sqlite")

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "interrupted"); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), c.active); err != nil {
		t.Fatal(err)
	}

	id := a.stages[lexicalTestScope].id

	if err := lifecycle.AbortProjectionSync(t.Context(), "interrupted"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("portable dirty checkpoint lost retained physical build: %d %v", count, err)
	}

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "replacement"); err != nil {
		t.Fatal("portable dirty checkpoint leaked owner", err)
	}
}

func TestPublicationAbortRetainsPortableSupersededCheckpointWithoutWalker(t *testing.T) {
	store, _ := openTestStore(t, false)
	previous := publicationManifest("previous", 7)
	previous.Status = memory.ProjectionGenerationSuperseded
	c := &publicationCheckpoints{active: publicationManifest("missing", 8), previous: []memory.ProjectionManifest{previous}}
	a, lifecycle := publicationLifecycle(t, store, &applyReader{}, c)
	lifecycle.ConfigureProjectionRetention(&publicationPlainCheckpoints{state: c}, "sqlite")

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "previous"); err != nil {
		t.Fatal(err)
	}

	if err := lifecycle.CommitProjectionManifest(t.Context(), previous); err != nil {
		t.Fatal(err)
	}

	id := a.stages[lexicalTestScope].id

	if err := lifecycle.AbortProjectionSync(t.Context(), "previous"); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := store.reader.QueryRow(`SELECT count(*) FROM lexical_build WHERE physical_build_id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("portable previous checkpoint lost physical build: %d %v", count, err)
	}

	if err := a.BeginProjectionRebuild(t.Context(), lexicalTestScope, "replacement"); err != nil {
		t.Fatal("portable previous checkpoint leaked owner", err)
	}
}
