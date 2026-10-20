package standaloneruntime //nolint:testpackage // Exercises native owner failures at actual runtime boundaries.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5"
)

func TestSQLRuntimeSecondCanonicalOwner(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		first, err := New(cfg)
		requireNoError(t, err, "first: %v")
		t.Cleanup(func() { _ = first.Stop(context.Background()) })

		other := cfg

		other.StateDir = filepath.Join(t.TempDir(), "other-state")
		if cfg.Canonical.Backend != "postgres" {
			other.Canonical.SQLite.Path = filepath.Join(cfg.StateDir, "canonical", "canonical.sqlite")
		}

		second, err := New(other)
		if err == nil {
			_ = second.Stop(t.Context())
			t.Fatal("second canonical owner acquired authority")
		}

		requireNoError(t, first.Start(t.Context()), "original remains usable: %v")
		requireNoError(t, first.Stop(t.Context()), "release first: %v")

		second, err = New(other)
		requireNoError(t, err, "partial constructor released local ownership: %v")
		requireNoError(t, second.Stop(t.Context()), "release second: %v")
	})
}

func TestSQLRuntimePostgresDisconnectReadiness(t *testing.T) {
	cfg := sqlRuntimeConfig(t, "postgres")
	runtime, err := New(cfg)
	requireNoError(t, err, "new: %v")
	t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
	requireNoError(t, runtime.Start(t.Context()), "start: %v")

	if !runtime.Ready() {
		t.Fatal("healthy runtime not ready")
	}

	control, err := pgx.Connect(t.Context(), cfg.Canonical.Postgres.DSN)
	requireNoError(t, err, "owned database control: %v")

	defer control.Close(context.Background())

	var pid int
	requireNoError(t, control.QueryRow(t.Context(), `SELECT l.pid FROM pg_catalog.pg_locks l
 JOIN pg_catalog.pg_stat_activity a ON a.pid=l.pid
 WHERE a.datname=current_database() AND l.locktype='advisory'
 AND l.classid=1651472750 AND l.objid=1667329647 AND l.granted AND l.pid<>pg_backend_pid()`).
		Scan(&pid), "owned session: %v")

	var terminated bool
	requireNoError(t, control.QueryRow(t.Context(), `SELECT pg_terminate_backend(pid) FROM pg_catalog.pg_stat_activity
 WHERE pid=$1 AND datname=current_database() AND usename=current_user AND pid<>pg_backend_pid()`, pid).
		Scan(&terminated), "terminate owned session: %v")

	if !terminated {
		t.Fatal("owned session not terminated")
	}

	if runtime.Ready() {
		t.Fatal("disconnected owner remained ready")
	}

	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("poisoned HTTP readiness=%d", response.Code)
	}

	_, err = runtime.service.store.LoadScopeState(t.Context(), m.Scope{Key: "runtime:poisoned", Kind: m.ScopeKindPersonal})
	if code, _, _ := m.ClassifyError(err); code != m.CodeUnavailable {
		t.Fatalf("owner implicitly recovered: %v", err)
	}

	requireNoError(t, runtime.Stop(t.Context()), "stop poisoned owner: %v")
	runtime, err = New(cfg)
	requireNoError(t, err, "explicit reopen: %v")
	requireNoError(t, runtime.Start(t.Context()), "startup after reopen: %v")

	if !runtime.Ready() {
		t.Fatal("explicit recovered owner not ready")
	}
}

func TestSQLRuntimeCanceledConstruction(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		runtime, err := NewContext(ctx, cfg)
		if runtime != nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled construction=%v %v", runtime, err)
		}

		runtime, err = NewContext(t.Context(), cfg)
		requireNoError(t, err, "cancellation left no ownership: %v")
		requireNoError(t, runtime.Stop(t.Context()), "stop: %v")
	})
}

func TestSQLRuntimePreservesPopulatedOldBadger(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		oldPath := filepath.Join(cfg.StateDir, "badger")
		seedPopulatedOldBadger(t, oldPath)

		scope := m.Scope{Key: "old:identity", Kind: m.ScopeKindPersonal}
		before := hashOldRuntimeTree(t, oldPath)
		runtime, err := New(cfg)
		requireNoError(t, err, "fresh SQL: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "fresh start: %v")
		_, found, err := runtime.service.store.LoadCanonicalOperation(t.Context(), scope, "old-identity")
		requireNoError(t, err, "fresh identity lookup: %v")

		if found {
			t.Fatal("old Badger identity was silently imported")
		}

		requireNoError(t, runtime.Stop(t.Context()), "SQL shutdown: %v")

		after := hashOldRuntimeTree(t, oldPath)
		if before != after {
			t.Fatal("SQL lifecycle changed old Badger files")
		}

		t.Logf("old populated Badger tree SHA256 before=%s after=%s", before, after)
	})
}

func hashOldRuntimeTree(t *testing.T, root string) string {
	t.Helper()

	digest := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		name, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		_, _ = digest.Write([]byte(name + "\x00"))
		_, _ = digest.Write(data)

		return nil
	})
	requireNoError(t, err, "hash old tree: %v")

	return hex.EncodeToString(digest.Sum(nil))
}
