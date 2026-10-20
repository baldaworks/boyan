//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPostgresOwnerPermissionAndMigrationCleanup(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	config := PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true}
	// PostgreSQL's built-in read-only role cannot migrate. SET ROLE is scoped
	// only to this new fixture connection; no role or operator object is changed.
	denied := config

	denied.DSN += "&options=-c%20role%3Dpg_read_all_data"
	if owner, err := OpenPostgresOwner(t.Context(), denied); owner != nil || err == nil {
		t.Fatal("read-only role migrated")
	}

	owner := openPostgresTestOwner(t, config)
	rawExec(t, owner.connection, "UPDATE boyan_canonical.schema_migration SET checksum='unsupported'")
	rawExec(t, owner.connection, "CREATE TABLE boyan_canonical.prior_state(value TEXT)")
	rawExec(t, owner.connection, "INSERT INTO boyan_canonical.prior_state VALUES('preserved')")

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	for range 2 {
		failed, err := OpenPostgresOwner(t.Context(), config)
		if failed != nil {
			_ = failed.Close()

			t.Fatal("unsupported schema opened")
		}

		assertCode(t, err, memory.CodeInvalidDerived)
	}

	assertFailedPostgresOpenPreservedState(t, db)
}

func assertFailedPostgresOpenPreservedState(t *testing.T, db *canonicalsqltest.Database) {
	t.Helper()

	var (
		checksum string
		marker   []byte
	)

	err := db.Postgres.QueryRow(t.Context(), "SELECT checksum FROM boyan_canonical.schema_migration").Scan(&checksum)
	if err != nil {
		t.Fatal(err)
	}

	err = db.Postgres.QueryRow(t.Context(), "SELECT value FROM boyan_canonical.prior_state").Scan(&marker)
	if err != nil {
		t.Fatal(err)
	}

	if checksum != "unsupported" || string(marker) != "preserved" {
		t.Fatal("failed migration changed durable state")
	}

	var count int

	err = db.Postgres.QueryRow(t.Context(), "SELECT count(*) FROM pg_catalog.pg_stat_activity "+
		"WHERE datname=current_database() AND pid<>pg_backend_pid()").Scan(&count)
	if err != nil || count != 0 {
		t.Fatalf("partial open leaked a session: count=%d err=%v", count, err)
	}
}

func openPostgresTestOwner(t *testing.T, config PostgresConfig) *Owner {
	t.Helper()

	owner, err := OpenPostgresOwner(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := owner.Close(); err != nil {
			t.Error(err)
		}
	})

	return owner
}

func TestPostgresOwnerFixedSchemaAndSecondOwnerBeforeMigration(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	if _, err := db.Postgres.Exec(t.Context(),
		"CREATE TABLE public.scope_state(untrusted bytea)"); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Postgres.Exec(t.Context(),
		"CREATE FUNCTION public.length(bytea) RETURNS integer LANGUAGE sql IMMUTABLE AS 'SELECT 0'"); err != nil {
		t.Fatal(err)
	}

	config := PostgresConfig{DSN: db.DSN + "&search_path=public,pg_catalog", AllowLocalInsecure: true}

	owner := openPostgresTestOwner(t, config)
	if err := owner.CheckIntegrity(t.Context()); err != nil {
		t.Fatal(err)
	}

	rawExec(t, owner.connection, "UPDATE boyan_canonical.schema_migration SET checksum='unsupported'")

	second, err := OpenPostgresOwner(t.Context(), config)
	if second != nil {
		_ = second.Close()

		t.Fatal("second owner opened")
	}

	assertCode(t, err, memory.CodeUnavailable)
}

func TestPostgresOwnerConnectFailureRedactionAndTimeout(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)

	u, err := url.Parse(db.DSN)
	if err != nil {
		t.Fatal("parse owned test URL")
	}

	const secret = "private-owner-test-credential"

	u.User = url.UserPassword(secret, secret)
	config := PostgresConfig{DSN: u.String(), AllowLocalInsecure: true}

	owner, err := OpenPostgresOwner(t.Context(), config)
	if owner != nil || err == nil || memory.IsRetryable(err) ||
		strings.Contains(err.Error(), secret) || errors.Unwrap(err) != nil {
		t.Fatal("native connection failure was acknowledged or leaked private diagnostics")
	}

	config.DSN = db.DSN
	config.ConnectTimeout = time.Nanosecond

	owner, err = OpenPostgresOwner(t.Context(), config)
	if owner != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("bounded connection timeout lost context identity: %v", err)
	}

	config.ConnectTimeout = 0
	openPostgresTestOwner(t, config)
}
