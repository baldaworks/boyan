// Package storefixture runs cases through implemented public canonical owners.
// It is separate from the raw fixture package to avoid primitive-test cycles.
package storefixture

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

// Store is the implemented lifecycle shared by canonical owners. Canonical
// domain conformance belongs to the later tasks implementing those ports.
type Store interface {
	RestorePreflight(ctx context.Context) error
	Close() error
}

// Fixture supplies the real owner and its constructor for reopen/lock cases.
type Fixture struct {
	Backend canonicalsqltest.Backend
	// Database is the fixture-owned raw connection for corruption/fault checks.
	Database *canonicalsqltest.Database
	Store    Store
	Open     func(context.Context) (Store, error)
}

const privateDirectoryMode = 0o700

// New opens an independent public owner for one explicitly selected backend.
// It lets consumer matrices construct a fresh authority in each nested case.
func New(t *testing.T, backend canonicalsqltest.Backend) *Fixture {
	t.Helper()

	var db *canonicalsqltest.Database

	switch backend {
	case canonicalsqltest.Postgres:
		db = canonicalsqltest.PostgresDatabase(t)
	case canonicalsqltest.SQLite:
		var err error

		dir := filepath.Join(t.TempDir(), "canonical")
		if err := os.MkdirAll(dir, privateDirectoryMode); err != nil {
			t.Fatal(err)
		}

		db, err = canonicalsqltest.OpenSQLite(t.Context(), filepath.Join(dir, "canonical.sqlite"))
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Error(err)
			}
		})
	default:
		t.Fatalf("unsupported fixture backend %q", backend)
	}

	return openFixture(t, db)
}

// ForEach runs both public owner factories with real isolated databases and
// required PostgreSQL configuration validation inherited from the raw helper.
func ForEach(t *testing.T, run func(*testing.T, *Fixture)) {
	t.Helper()

	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		run(t, openFixture(t, db))
	})
}

func openFixture(t *testing.T, db *canonicalsqltest.Database) *Fixture {
	t.Helper()

	if db.SQLite != nil {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}

	open := func(ctx context.Context) (Store, error) {
		var (
			store Store
			err   error
		)
		if db.Backend == canonicalsqltest.SQLite {
			store, err = sqlite.Open(ctx, sqlite.Config{Path: db.DSN})
		} else {
			store, err = postgres.Open(ctx, postgres.Config{DSN: db.DSN, AllowLocalInsecure: true})
		}

		if err != nil {
			return nil, err
		}

		return store, nil
	}

	store, err := open(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})

	return &Fixture{Backend: db.Backend, Database: db, Store: store, Open: open}
}
