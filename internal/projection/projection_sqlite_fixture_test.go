package projection //nolint:testpackage // Shared real canonical/SQLite runtime fixtures.

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	index "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/pkg/memory"
)

type projectionSQLApplier struct {
	*index.SQLiteCanonicalApplier

	store *index.Store
	db    *sql.DB
}

type projectionCanonicalAuthority interface {
	IsSourceDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	IsRevisionDenied(ctx context.Context, scope memory.Scope, id string) (bool, error)
	LoadCanonicalRevisions(ctx context.Context,
		request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error)
}

func openProjectionSQLite(t *testing.T, root string, canonical projectionCanonicalAuthority,
	reader memory.RecallCanonicalReader) *projectionSQLApplier {
	t.Helper()

	dir := filepath.Join(root, "index")
	requireNoError(t, os.MkdirAll(dir, 0o700), "%v")
	store, err := index.Open(t.Context(), dir, index.Options{})
	requireNoError(t, err, "%v")
	applier, err := index.NewSQLiteCanonicalApplier(store, reader, canonical)
	requireNoError(t, err, "%v")
	db, err := sql.Open("sqlite", filepath.Join(dir, "search", "search.sqlite"))
	requireNoError(t, err, "%v")

	return &projectionSQLApplier{SQLiteCanonicalApplier: applier, store: store, db: db}
}

func (a *projectionSQLApplier) Close() error {
	if err := a.db.Close(); err != nil {
		return err
	}

	return a.store.Close()
}

func (a *projectionSQLApplier) countGeneration(t *testing.T, generation string) int {
	t.Helper()

	var count int

	const query = "SELECT count(*) FROM lexical_build WHERE generation_id=?"
	requireNoError(t, a.db.QueryRowContext(context.Background(), query, generation).Scan(&count), "%v")

	return count
}

func (a *projectionSQLApplier) activeManifest(t *testing.T, scope memory.Scope) memory.ProjectionManifest {
	t.Helper()
	manifest, found, err := a.store.LoadActivePhysicalProjectionManifest(t.Context(), scope, "sqlite")
	requireNoError(t, err, "%v")

	if !found {
		t.Fatal("active physical manifest missing")
	}

	return manifest
}
