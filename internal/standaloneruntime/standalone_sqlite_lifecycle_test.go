package standaloneruntime //nolint:testpackage // Exercises shared SQL drain and optional initialization failure.

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func openRuntimeSQL(t *testing.T, runtime *Runtime) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", "file:"+filepath.Join(runtime.service.state.Root, "search", "search.sqlite")+"?mode=rw")
	requireNoError(t, err, "open fault connection: %v")
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// A semantic-only SQL failure must not escape through lexical readiness or
// leak the trigger's private prose. A retry uses the same Manager barrier.
func TestSQLiteRuntimeSemanticInitializerFailureKeepsLexicalReady(t *testing.T) {
	provider := &lifecycleEmbeddingProvider{}
	runtime, err := New(enabledLifecycleConfig(t, provider))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "initializer")
	db := openRuntimeSQL(t, runtime)
	_, err = db.ExecContext(t.Context(), `INSERT INTO semantic_build
 (build_id,scope_kind,scope_key,generation_id,physical_build_id,watermark,model_space,dimensions,epoch,status)
 VALUES('orphan','personal','orphan','generation','physical',zeroblob(8),'model',2,zeroblob(8),'building');
 CREATE TRIGGER fail_semantic_repair BEFORE DELETE ON semantic_build
 BEGIN SELECT RAISE(ABORT,'private SQL fault sentinel'); END`)
	requireNoError(t, err, "install semantic fault: %v")
	startSQLComponents(t, runtime)
	runtime.service.semantic.startup.Wait()
	status, err := runtime.SemanticStatus(t.Context(), turn.Scope)
	requireNoError(t, err, "safe initialization status: %v")

	if status.State != "degraded" || status.Reason == "" ||
		strings.Contains(status.Reason, "private") || !runtime.Ready() {
		t.Fatalf("unsafe initialization failure: %+v ready=%t", status, runtime.Ready())
	}

	if provider.calls.Load() != 0 {
		t.Fatal("failed initializer admitted provider work")
	}

	assertSQLRecall(t, runtime, turn.Scope)
	_, err = db.ExecContext(t.Context(), "DROP TRIGGER fail_semantic_repair")
	requireNoError(t, err, "remove fault: %v")
	runtime.service.semantic.start(runtime.service.shutdownContext)
	runtime.service.semantic.startup.Wait()
	waitSemanticState(t, runtime, turn.Scope, "hybrid-ready")

	if got := runtime.service.semantic.summary(); got != "hybrid-ready" {
		t.Fatalf("successful initialization retry left stale status %q", got)
	}
}

func TestSQLiteRuntimeUnrepairableLexicalFailureHasRecoveryGuidance(t *testing.T) {
	runtime, err := New(testStandaloneConfig(t))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	seedSemanticTurn(t, runtime, "required-repair")
	requireNoError(t, runtime.service.index.Close(), "inject required storage failure: %v")

	err = runtime.service.startComponentsUntilShutdown(t.Context())
	if err == nil || !strings.Contains(err.Error(), "back up") ||
		!strings.Contains(err.Error(), "rebuild") || runtime.Ready() {
		t.Fatalf("required repair failed without safe guidance: err=%v ready=%t", err, runtime.Ready())
	}
}

func TestSQLiteRuntimeFailedStageReleasesOwnedCandidate(t *testing.T) {
	runtime, err := New(enabledLifecycleConfig(t, &lifecycleEmbeddingProvider{}))
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "stage-fault")
	db := openRuntimeSQL(t, runtime)
	_, err = db.ExecContext(t.Context(), `CREATE TRIGGER fail_semantic_stage BEFORE INSERT ON semantic_chunk
 BEGIN SELECT RAISE(ABORT,'private stage fault'); END`)
	requireNoError(t, err, "install Stage fault: %v")
	startSQLComponents(t, runtime)
	runtime.service.semantic.startup.Wait()
	waitSemanticState(t, runtime, turn.Scope, "degraded")

	var pending int

	err = db.QueryRowContext(t.Context(), "SELECT count(*) FROM semantic_build WHERE epoch=zeroblob(8)").Scan(&pending)
	requireNoError(t, err, "owned candidate cleanup: %v")

	if pending != 0 {
		t.Fatalf("failed Stage retained %d owned candidates", pending)
	}

	assertSQLRecall(t, runtime, turn.Scope)
}

type stubbornEmbeddingProvider struct {
	entered chan struct{}
	release chan struct{}
}

func (provider stubbornEmbeddingProvider) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	close(provider.entered)
	<-provider.release

	vectors := make([][]float32, len(inputs))
	for i := range vectors {
		vectors[i] = []float32{1, 0}
	}

	return vectors, nil
}

// Closing shared SQL before the Manager flight/terminal guard drains prevents
// local reads while this noncooperative provider is still returning.
func TestSQLiteRuntimeShutdownDeadlineRetainsSharedStorage(t *testing.T) {
	provider := stubbornEmbeddingProvider{entered: make(chan struct{}), release: make(chan struct{})}
	cfg := enabledLifecycleConfig(t, provider)
	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	turn := seedSemanticTurn(t, runtime, "provider-drain")
	startSQLComponents(t, runtime)

	select {
	case <-provider.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("provider did not start")
	}

	var releaseOnce sync.Once

	release := func() { releaseOnce.Do(func() { close(provider.release) }) }
	t.Cleanup(release)

	deadline, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()

	if err := runtime.Stop(deadline); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop did not preserve caller deadline: %v", err)
	}

	if runtime.Ready() {
		t.Fatal("stopping runtime remained ready")
	}

	_, found, err := runtime.service.index.LoadActivePhysicalProjectionManifest(t.Context(), turn.Scope, "sqlite")
	requireNoError(t, err, "SQL closed under provider flight: %v")

	if !found {
		t.Fatal("SQL certificate disappeared during drain")
	}

	_, err = runtime.service.store.LoadScopeState(t.Context(), turn.Scope)
	requireNoError(t, err, "canonical closed under provider flight: %v")
	release()
	requireNoError(t, runtime.Stop(t.Context()), "finish drain: %v")

	restarted, err := New(cfg)
	requireNoError(t, err, "storage was not released after drain: %v")
	requireNoError(t, restarted.Stop(t.Context()), "reopened stop: %v")
}
