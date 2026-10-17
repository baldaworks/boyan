package standaloneruntime //nolint:testpackage // Verifies actual runtime storage and lifecycle ownership.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/internal/projection"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Keeping the legacy runtime graph loses the SQL file, additive certificate and
// recovery from canonical state asserted here.
func TestSQLiteRuntimeReopenAndMissingProjectionRecovery(t *testing.T) {
	cfg := testStandaloneConfig(t)
	legacy := []string{filepath.Join(cfg.StateDir, "bleve", "operator-backup"),
		filepath.Join(cfg.StateDir, "embeddings", "operator-backup")}

	for _, path := range legacy {
		requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700), "legacy directory: %v")
		requireNoError(t, os.WriteFile(path, []byte("private legacy projection"), 0o600), "legacy fixture: %v")
	}

	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "sqlite-runtime")
	legacyManifest, receipt := seedLegacyCheckpoint(t, runtime, turn)
	startSQLComponents(t, runtime)
	assertSQLRecall(t, runtime, turn.Scope)
	manifest, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "sqlite checkpoint: %v")

	if !found || manifest.Watermark == 0 {
		t.Fatalf("missing additive SQLite certificate: %+v", manifest)
	}

	assertSQLSemanticTables(t, cfg.StateDir, 0)
	requireNoError(t, runtime.Stop(context.Background()), "stop: %v")

	for _, missing := range []bool{false, true} {
		if missing {
			// Explicit operator removal of this disposable test fixture models a
			// lost projection; the runtime itself must never delete old files.
			requireNoError(t, os.Remove(filepath.Join(cfg.StateDir, "search", "search.sqlite")), "remove SQL fixture: %v")
		}

		runtime, err = New(cfg)
		requireNoError(t, err, "reopen: %v")
		startSQLComponents(t, runtime)
		assertSQLRecall(t, runtime, turn.Scope)
		assertLegacyAuthority(t, runtime, turn, legacyManifest, receipt)
		requireNoError(t, runtime.Stop(context.Background()), "reopened stop: %v")
	}

	for _, path := range legacy {
		bytes, err := os.ReadFile(path)
		requireNoError(t, err, "legacy bytes: %v")

		if string(bytes) != "private legacy projection" {
			t.Fatal("legacy projection changed")
		}
	}
}

func seedLegacyCheckpoint(t *testing.T, runtime *Runtime, turn memory.Turn) (
	memory.ProjectionManifest, memory.CanonicalOperationRecord) {
	t.Helper()
	state, err := runtime.service.store.LoadScopeState(t.Context(), turn.Scope)
	requireNoError(t, err, "scope before migration: %v")

	manifest := memory.ProjectionManifest{Scope: turn.Scope, ProjectionID: "bleve", GenerationID: "legacy",
		Status: memory.ProjectionGenerationDirty, Watermark: state.ChangeSeq, UpdatedAt: time.Now().UTC()}
	requireNoError(t, runtime.service.store.MarkProjectionDirty(t.Context(), manifest), "legacy checkpoint: %v")
	err = runtime.service.store.ActivateProjectionGeneration(t.Context(), manifest, manifest.UpdatedAt)
	requireNoError(t, err, "activate legacy: %v")

	manifest.Status = memory.ProjectionGenerationActive
	operation, err := memory.CanonicalSemanticOperationID(turn.ExportID, memory.LegacyDerivationRef())
	requireNoError(t, err, "operation identity: %v")
	receipt, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
	requireNoError(t, err, "canonical receipt: %v")

	if !found {
		t.Fatal("seed receipt missing")
	}

	return manifest, receipt
}

func assertLegacyAuthority(t *testing.T, runtime *Runtime, turn memory.Turn,
	wantManifest memory.ProjectionManifest, wantReceipt memory.CanonicalOperationRecord) {
	t.Helper()
	manifest, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "bleve")
	requireNoError(t, err, "preserved legacy checkpoint: %v")

	if !found || manifest != wantManifest {
		t.Fatalf("legacy certificate changed: got=%+v want=%+v", manifest, wantManifest)
	}

	operation, err := memory.CanonicalSemanticOperationID(turn.ExportID, memory.LegacyDerivationRef())
	requireNoError(t, err, "operation identity: %v")
	receipt, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), turn.Scope, operation)
	requireNoError(t, err, "preserved receipt: %v")

	if !found || !reflect.DeepEqual(receipt, wantReceipt) {
		t.Fatalf("canonical receipt changed: got=%+v want=%+v", receipt, wantReceipt)
	}
}

func TestSQLiteRuntimeOptionalSemanticSharesStorage(t *testing.T) {
	provider := &lifecycleEmbeddingProvider{}
	cfg := enabledLifecycleConfig(t, provider)
	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "shared")
	startSQLComponents(t, runtime)
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")
	assertSQLSemanticTables(t, cfg.StateDir, 6)
	response, err := runtime.Search(t.Context(), memory.RecallRequest{Scope: turn.Scope, Query: "Bishkek", Limit: 10})
	requireNoError(t, err, "hybrid recall: %v")

	if len(response.Results) != 1 || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" {
		t.Fatalf("uncertified shared recall: %+v", response)
	}

	if _, err := os.Stat(filepath.Join(cfg.StateDir, "embeddings")); !os.IsNotExist(err) {
		t.Fatalf("separate semantic storage created: %v", err)
	}

	// Disabled reopen ignores old semantic content even when its vector norm
	// is invalid. Required relational/lexical integrity still remains checked.
	db := openRuntimeSQL(t, runtime)
	_, err = db.ExecContext(t.Context(), "UPDATE semantic_chunk SET embedding=zeroblob(8)")
	requireNoError(t, err, "invalidate old vectors: %v")
	requireNoError(t, db.Close(), "close fault connection: %v")
	requireNoError(t, runtime.Stop(context.Background()), "stop: %v")

	cfg.Embeddings = EmbeddingsConfig{}

	calls := provider.calls.Load()
	runtime, err = New(cfg)
	requireNoError(t, err, "disabled reopen: %v")
	startSQLComponents(t, runtime)
	assertSQLRecall(t, runtime, turn.Scope)

	if runtime.service.semantic != nil || provider.calls.Load() != calls {
		t.Fatal("disabled reopen admitted semantic work")
	}

	requireNoError(t, runtime.Stop(context.Background()), "disabled stop: %v")
}

func TestSQLiteRuntimeCorruptRequiredFileIsPreserved(t *testing.T) {
	cfg := testStandaloneConfig(t)
	path := filepath.Join(cfg.StateDir, "search", "search.sqlite")
	requireNoError(t, os.MkdirAll(filepath.Dir(path), 0o700), "search directory: %v")

	corrupt := []byte("invalid private SQLite header")
	requireNoError(t, os.WriteFile(path, corrupt, 0o600), "corrupt fixture: %v")

	runtime, err := New(cfg)
	if err == nil {
		_ = runtime.Stop(context.Background())

		t.Fatal("required corrupt SQL file was ignored")
	}

	if !strings.Contains(err.Error(), "back up") || !strings.Contains(err.Error(), "rebuild") {
		t.Fatalf("missing recovery guidance: %v", err)
	}

	after, err := os.ReadFile(path)
	requireNoError(t, err, "preserved file: %v")

	if sha256.Sum256(after) != sha256.Sum256(corrupt) {
		t.Fatal("corrupt operator file changed")
	}
}

type rebuildSignalCanonical struct {
	memory.CanonicalStore

	sequence uint64
}

type retainedSQLApplier struct {
	*sqlite.SQLiteCanonicalApplier

	check func()
}

func (applier retainedSQLApplier) BeginProjectionRebuild(ctx context.Context,
	scope memory.Scope, generation string) error {
	applier.check()

	return applier.SQLiteCanonicalApplier.BeginProjectionRebuild(ctx, scope, generation)
}

func (store rebuildSignalCanonical) ScanScopeChanges(ctx context.Context, scope memory.Scope,
	after uint64, limit uint32) ([]memory.ScopeChange, error) {
	changes, err := store.CanonicalStore.ScanScopeChanges(ctx, scope, after, limit)
	for i := range changes {
		if changes[i].Sequence == store.sequence {
			// Fault at the canonical-port boundary reproduces the durable marker
			// emitted by a large forget, after an earlier real SQL delta commit.
			changes[i].RequiresRebuild = true
		}
	}

	return changes, err
}

// Starting a replacement before aborting the resumed SQL session conflicts
// with the actual applier's exact scope ownership, leaving repair unavailable.
func TestSQLiteRuntimeResumedDeltaAbortsBeforeReplacement(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "original")
	startSQLComponents(t, runtime)
	seedSemanticTurn(t, runtime, "previous")
	requireNoError(t, runtime.service.projection.Sync(t.Context(), turn.Scope), "establish previous view: %v")
	db := openRuntimeSQL(t, runtime)
	activeID, previousID := sqlRetainedViews(t, db, turn.Scope)
	before, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "original manifest: %v")

	if !found {
		t.Fatal("original manifest missing")
	}

	seedSemanticTurn(t, runtime, "delta")
	seedSemanticTurn(t, runtime, "marker")
	state, err := runtime.service.store.LoadScopeState(t.Context(), turn.Scope)
	requireNoError(t, err, "scope state: %v")
	syncer, err := projection.NewProjectionRuntime(rebuildSignalCanonical{
		CanonicalStore: runtime.service.store, sequence: state.ChangeSeq,
	}, runtime.service.store, retainedSQLApplier{SQLiteCanonicalApplier: runtime.service.applier, check: func() {
		active, previous := sqlRetainedViews(t, db, turn.Scope)
		if active != activeID || previous != previousID {
			t.Fatalf("abort changed retained views: active=%q previous=%q", active, previous)
		}
	}}, "sqlite", 1)
	requireNoError(t, err, "small-page runtime: %v")
	t.Cleanup(func() { _ = syncer.Close(context.Background()) })
	requireNoError(t, syncer.Sync(t.Context(), turn.Scope), "replace resumed generation: %v")
	after, found, err := runtime.service.store.LoadActiveProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "replacement manifest: %v")

	if !found || before.GenerationID == after.GenerationID || after.Watermark != state.ChangeSeq {
		t.Fatalf("replacement was not certified: before=%+v after=%+v", before, after)
	}

	response, err := runtime.Search(t.Context(), memory.RecallRequest{Scope: turn.Scope, Query: "Bishkek", Limit: 10})
	requireNoError(t, err, "replacement recall: %v")

	if len(response.Results) != 4 {
		t.Fatalf("rebuild lost canonical records: %+v", response)
	}
}

func sqlRetainedViews(t *testing.T, db *sql.DB, scope memory.Scope) (string, string) {
	t.Helper()

	var active, previous string

	err := db.QueryRowContext(t.Context(), `SELECT a.physical_build_id,p.physical_build_id
 FROM lexical_active a JOIN lexical_previous p USING(scope_kind,scope_key)
 WHERE a.scope_kind=? AND a.scope_key=?`, scope.Kind, scope.Key).Scan(&active, &previous)
	requireNoError(t, err, "retained SQL views: %v")

	if active == previous || active == "" || previous == "" {
		t.Fatalf("distinct retained views missing: active=%q previous=%q", active, previous)
	}

	return active, previous
}

func startSQLComponents(t *testing.T, runtime *Runtime) {
	t.Helper()
	// Exercise the production startup/readiness graph without opening a network
	// listener; the existing Runtime.Start tests cover listener ownership.
	requireNoError(t, runtime.service.startComponentsUntilShutdown(t.Context()), "start components: %v")
	runtime.service.finishStart()

	if !runtime.Ready() {
		t.Fatal("repaired lexical runtime is not ready")
	}
}

func assertSQLRecall(t *testing.T, runtime *Runtime, scope memory.Scope) {
	t.Helper()
	response, err := runtime.Search(t.Context(), memory.RecallRequest{Scope: scope, Query: "Bishkek", Limit: 10})
	requireNoError(t, err, "recall: %v")

	if len(response.Results) != 1 || !strings.Contains(response.Results[0].Text, "Bishkek home") {
		t.Fatalf("canonical recall lost: %+v", response)
	}
}

func assertSQLSemanticTables(t *testing.T, root string, want int) {
	t.Helper()

	path := filepath.Join(root, "search", "search.sqlite")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("required SQL projection missing: %v", err)
	}

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	requireNoError(t, err, "inspect SQL: %v")

	defer func() { _ = db.Close() }()

	var count int

	const tables = "SELECT count(*) FROM sqlite_schema WHERE type='table' AND name LIKE 'semantic_%'"

	err = db.QueryRowContext(t.Context(), tables).Scan(&count)
	requireNoError(t, err, "semantic table enumeration: %v")

	if count != want {
		t.Fatalf("semantic tables=%d, want %d", count, want)
	}
}
