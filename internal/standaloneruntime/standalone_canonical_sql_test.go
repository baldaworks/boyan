package standaloneruntime //nolint:testpackage // Verifies the runtime's selected authority and lifecycle.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	canonicalpostgres "github.com/baldaworks/boyan/store/postgres"
	canonicalsqlite "github.com/baldaworks/boyan/store/sqlite"
)

func forEachSQLRuntimeConfig(t *testing.T, run func(*testing.T, Config)) {
	t.Helper()

	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			run(t, sqlRuntimeConfig(t, backend))
		})
	}
}

func sqlRuntimeConfig(t *testing.T, backend string) Config {
	t.Helper()

	cfg := testStandaloneConfig(t)
	if backend == "postgres" {
		db := canonicalsqltest.PostgresDatabase(t)
		cfg.Canonical = CanonicalConfig{Backend: backend, Postgres: canonicalpostgres.Config{
			DSN: db.DSN, AllowLocalInsecure: true}}
	}

	return cfg
}

func TestSQLRuntimeSelection(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new SQL runtime: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })

		switch any(runtime.service.store).(type) {
		case *canonicalsqlite.Store:
			if cfg.Canonical.Backend == "postgres" {
				t.Fatal("PostgreSQL selection fell back to SQLite")
			}

			_, err := os.Stat(filepath.Join(cfg.StateDir, "canonical", "canonical.sqlite"))
			requireNoError(t, err, "default canonical SQLite path: %v")
		case *canonicalpostgres.Store:
			if cfg.Canonical.Backend != "postgres" {
				t.Fatal("default selection opened PostgreSQL")
			}
		default:
			t.Fatalf("runtime authority = %T, want selected public SQL Store", runtime.service.store)
		}

		requireNoError(t, runtime.Start(t.Context()), "start SQL runtime: %v")

		if !runtime.Ready() {
			t.Fatal("successful SQL startup is not ready")
		}

		requireNoError(t, runtime.Stop(t.Context()), "stop SQL runtime: %v")
	})
}

func TestSQLRuntimeLocalSearchOwnership(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		first, err := New(cfg)
		requireNoError(t, err, "first owner: %v")
		t.Cleanup(func() { _ = first.Stop(context.Background()) })
		// A distinct canonical authority still conflicts with shared local search.
		other := cfg
		other.Canonical = CanonicalConfig{Backend: "sqlite", SQLite: canonicalsqlite.Config{
			Path: filepath.Join(t.TempDir(), "other.sqlite")}}

		second, err := New(other)
		if err == nil {
			_ = second.Stop(t.Context())
			t.Fatal("two authorities acquired the same local search state")
		}

		requireNoError(t, first.Stop(t.Context()), "stop owner: %v")

		second, err = New(other)
		requireNoError(t, err, "released local search ownership: %v")
		requireNoError(t, second.Stop(t.Context()), "stop second: %v")
	})
}

func TestSQLRuntimeClosedAuthorityRevokesReadiness(t *testing.T) {
	forEachSQLRuntimeConfig(t, func(t *testing.T, cfg Config) {
		runtime, err := New(cfg)
		requireNoError(t, err, "new: %v")
		t.Cleanup(func() { _ = runtime.Stop(context.Background()) })
		requireNoError(t, runtime.Start(t.Context()), "start: %v")

		if !runtime.Ready() {
			t.Fatal("healthy authority not ready")
		}

		requireNoError(t, runtime.service.store.Close(), "close authority: %v")

		if runtime.Ready() {
			t.Fatal("closed authority still advertised as ready")
		}
	})
}
