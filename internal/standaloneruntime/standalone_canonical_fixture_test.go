//go:build integration

package standaloneruntime //nolint:testpackage // SQL fixture mutation remains outside runtime ports.

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5"
)

type runtimeFixtureStore interface {
	standaloneCanonicalStore
	BeginScopeErase(ctx context.Context, request memory.ScopeEraseRequest) (memory.ScopeEraseJournal, error)
	DenySource(ctx context.Context, scope memory.Scope, sourceID string, at time.Time) error
}

func runtimeStore(t *testing.T, runtime *Runtime) runtimeFixtureStore {
	t.Helper()

	store, ok := runtime.service.store.(runtimeFixtureStore)
	if !ok {
		t.Fatal("selected SQL store lacks fixture mutation ports")
	}

	return store
}

func openRuntimeCanonicalSQL(t *testing.T, cfg Config) (*sql.DB, *canonicalsql.Connection) {
	t.Helper()

	path := cfg.Canonical.SQLite.Path
	if path == "" {
		path = filepath.Join(cfg.StateDir, "canonical", "canonical.sqlite")
	}

	db, err := sql.Open("sqlite", path)
	requireNoError(t, err, "raw canonical SQL: %v")
	t.Cleanup(func() { _ = db.Close() })
	conn, err := db.Conn(t.Context())
	requireNoError(t, err, "raw canonical connection: %v")
	t.Cleanup(func() { _ = conn.Close() })

	return db, canonicalsql.NewSQLite(conn)
}

func openFixtureSQLStore(t *testing.T, cfg Config) runtimeFixtureStore {
	t.Helper()

	layout, err := buildStateLayout(cfg.StateDir)
	requireNoError(t, err, "fixture layout: %v")
	store, _, err := openStandaloneCanonical(t.Context(), layout, cfg.Canonical)
	requireNoError(t, err, "fixture store: %v")

	return store.(runtimeFixtureStore)
}

type runtimeNativeProbe struct {
	connection *canonicalsql.Connection
	exec       func(string, ...any) error
	table      func(string) string
}

func nativeRuntimeProbe(t *testing.T, cfg Config) runtimeNativeProbe {
	t.Helper()

	if cfg.Canonical.Backend == "postgres" {
		conn, err := pgx.Connect(t.Context(), cfg.Canonical.Postgres.DSN)
		requireNoError(t, err, "fixture connection: %v")
		t.Cleanup(func() { _ = conn.Close(context.Background()) })

		return runtimeNativeProbe{connection: canonicalsql.NewPostgres(conn),
			exec: func(statement string, args ...any) error {
				_, err := conn.Exec(t.Context(), statement, args...)

				return err
			},
			table: func(name string) string { return "boyan_canonical." + name }}
	}

	db, connection := openRuntimeCanonicalSQL(t, cfg)

	return runtimeNativeProbe{connection: connection,
		exec: func(statement string, args ...any) error {
			_, err := db.ExecContext(t.Context(), statement, args...)

			return err
		},
		table: func(name string) string { return name }}
}

func failRuntimePrivacyPreparation(t *testing.T, cfg Config) func() {
	t.Helper()

	p := nativeRuntimeProbe(t, cfg)
	if cfg.Canonical.Backend == "postgres" {
		requireNoError(t, p.exec(`CREATE FUNCTION boyan_canonical.task17_fail() RETURNS trigger
 LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'task17 preparation fault'; END $$`), "fault function: %v")
		requireNoError(t, p.exec(`CREATE TRIGGER task17_fail BEFORE INSERT ON boyan_canonical.source_privacy_ready
 FOR EACH ROW EXECUTE FUNCTION boyan_canonical.task17_fail()`), "fault trigger: %v")

		return func() {
			requireNoError(t, p.exec("DROP TRIGGER task17_fail ON boyan_canonical.source_privacy_ready"), "remove fault: %v")
			requireNoError(t, p.exec("DROP FUNCTION boyan_canonical.task17_fail()"), "remove function: %v")
		}
	}

	requireNoError(t, p.exec(`CREATE TRIGGER task17_fail BEFORE INSERT ON source_privacy_ready
 BEGIN SELECT RAISE(ABORT,'task17 preparation fault'); END`), "fault trigger: %v")

	return func() { requireNoError(t, p.exec("DROP TRIGGER task17_fail"), "remove fault: %v") }
}
