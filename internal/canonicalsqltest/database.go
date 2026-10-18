// Package canonicalsqltest supplies isolated real SQL connections for canonical
// storage tests. Public owner factories live in its storefixture subpackage.
package canonicalsqltest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "modernc.org/sqlite" // Register the real SQLite driver for raw test fixtures.
)

const fixtureTimeout = 10 * time.Second

// Backend names one real SQL engine; it is not a canonical store registration.
type Backend string

const (
	// SQLite selects a temporary on-disk SQLite database.
	SQLite Backend = "sqlite"
	// Postgres selects a newly created disposable PostgreSQL database.
	Postgres Backend = "postgres"
)

// Database owns a disposable raw database, independent of the caller's database.
type Database struct {
	Backend  Backend
	SQLite   *sql.Conn
	Postgres *pgx.Conn
	// DSN identifies only this disposable database. It may contain credentials;
	// never include it in test logs or assertion messages.
	DSN string

	mu           sync.Mutex
	pool         *sql.DB
	admin        *pgx.Conn
	databaseName string
	databaseOID  string
	ownerOID     string
	closed       bool
}

func configuredPostgres() (string, error) {
	required := false

	if value := os.Getenv("BOYAN_REQUIRE_POSTGRES"); value != "" {
		var err error

		required, err = strconv.ParseBool(value)
		if err != nil {
			return "", errors.New("BOYAN_REQUIRE_POSTGRES must be a boolean")
		}
	}

	dsn := strings.TrimSpace(os.Getenv("BOYAN_TEST_POSTGRES_DSN"))
	if required && dsn == "" {
		return "", errors.New("BOYAN_REQUIRE_POSTGRES requires BOYAN_TEST_POSTGRES_DSN")
	}

	return dsn, nil
}

// PostgresDatabase creates a required-mode-aware fixture for PostgreSQL-only
// physical/session tests without manufacturing a no-op SQLite test case.
func PostgresDatabase(t *testing.T) *Database {
	t.Helper()

	dsn, err := configuredPostgres()
	if err != nil {
		t.Fatal(err)
	}

	if dsn == "" {
		t.Skip("set BOYAN_TEST_POSTGRES_DSN; CI requires BOYAN_REQUIRE_POSTGRES=1")
	}

	db, err := OpenPostgres(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})

	return db
}

// ForEach runs a raw SQL case with independent SQLite and PostgreSQL fixtures.
// Local PostgreSQL absence skips that subtest only; required mode fails closed.
// A configured but unreachable server always fails, even in optional mode.
func ForEach(t *testing.T, run func(*testing.T, *Database)) {
	t.Helper()

	dsn, err := configuredPostgres()
	if err != nil {
		t.Fatal(err)
	}

	for _, backend := range []Backend{SQLite, Postgres} {
		t.Run(string(backend), func(t *testing.T) {
			if backend == Postgres && dsn == "" {
				t.Skip("set BOYAN_TEST_POSTGRES_DSN; CI requires BOYAN_REQUIRE_POSTGRES=1")
			}

			var (
				db  *Database
				err error
			)
			if backend == SQLite {
				db, err = OpenSQLite(t.Context(), filepath.Join(t.TempDir(), "canonical.sqlite"))
			} else {
				db, err = OpenPostgres(t.Context(), dsn)
			}

			if err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if err := db.Close(); err != nil {
					t.Error(err)
				}
			})
			run(t, db)
		})
	}
}

// OpenSQLite opens a real file owned by the caller's temporary directory.
// Close closes its native connection and pool; testing owns directory removal.
func OpenSQLite(ctx context.Context, path string) (*Database, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite fixture path is required")
	}

	pool, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.New("open sqlite fixture")
	}

	pool.SetMaxOpenConns(1)

	conn, err := pool.Conn(ctx)
	if err != nil {
		_ = pool.Close()

		return nil, errors.New("connect sqlite fixture")
	}

	db := &Database{Backend: SQLite, SQLite: conn, pool: pool, DSN: path}

	pragmas := []string{
		"PRAGMA journal_mode=WAL", "PRAGMA synchronous=FULL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=1000",
	}
	for _, statement := range pragmas {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			_ = db.Close()

			return nil, errors.New("configure sqlite fixture")
		}
	}

	return db, nil
}

// OpenPostgres creates a disposable database using an explicit test connection.
func OpenPostgres(ctx context.Context, dsn string) (*Database, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("postgres fixture DSN is required")
	}

	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("parse postgres fixture connection configuration")
	}

	ctx, cancel := context.WithTimeout(ctx, fixtureTimeout)
	defer cancel()

	admin, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, errors.New("connect postgres fixture maintenance database")
	}

	name, err := fixtureDatabaseName()
	if err != nil {
		_ = admin.Close(ctx)

		return nil, err
	}

	db := &Database{Backend: Postgres, admin: admin, databaseName: name}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()+" TEMPLATE template0"); err != nil {
		_ = admin.Close(ctx)

		return nil, errors.New("create isolated postgres fixture database (test role requires CREATEDB)")
	}

	identity := admin.QueryRow(ctx, "SELECT oid::text, datdba::text FROM pg_database WHERE datname=$1", name)
	if err := identity.Scan(&db.databaseOID, &db.ownerOID); err != nil {
		// Unknown identity must never authorize DROP, including a setup failure.
		_ = admin.Close(ctx)

		return nil, fmt.Errorf("identify newly created postgres fixture %s; database retained for inspection", name)
	}

	db.DSN = fixtureDSN(dsn, name)

	config, err = pgx.ParseConfig(db.DSN)
	if err != nil {
		return nil, errors.Join(errors.New("configure disposable postgres fixture"), db.Close())
	}

	db.Postgres, err = pgx.ConnectConfig(ctx, config)
	if err == nil {
		_, err = db.Postgres.Exec(ctx, "CREATE SCHEMA boyan_canonical")
	}

	if err != nil {
		cleanupErr := db.Close()

		return nil, errors.Join(errors.New("initialize isolated postgres fixture database"), cleanupErr)
	}

	return db, nil
}

// Close releases raw connections and removes only the exact PostgreSQL database
// created by this fixture. An identity mismatch retains the database and allows
// cleanup to be retried after the mismatch is investigated.
func (db *Database) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if db.closed {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), fixtureTimeout)
	defer cancel()

	if db.SQLite != nil {
		err := errors.Join(db.SQLite.Close(), db.pool.Close())
		db.closed = true

		return err
	}

	if db.Postgres != nil {
		if err := db.Postgres.Close(ctx); err != nil {
			return errors.New("close postgres fixture connection")
		}
	}

	if err := db.dropOwnedDatabase(ctx); err != nil {
		return err
	}

	db.closed = true
	if err := db.admin.Close(ctx); err != nil {
		return errors.New("close postgres fixture maintenance connection")
	}

	return nil
}
