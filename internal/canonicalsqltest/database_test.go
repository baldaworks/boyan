package canonicalsqltest //nolint:testpackage // Exercises private cleanup identity guards with real connections.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Missing required PostgreSQL must fail the matrix rather than produce a green skip.
func TestRequiredPostgresConfiguration(t *testing.T) {
	t.Setenv("BOYAN_REQUIRE_POSTGRES", "1")
	t.Setenv("BOYAN_TEST_POSTGRES_DSN", "")

	if _, err := configuredPostgres(); err == nil || !strings.Contains(err.Error(), "BOYAN_TEST_POSTGRES_DSN") {
		t.Fatalf("missing required DSN: %v", err)
	}
}

// Removing rollback or committing a failed transaction must leave a visible row.
//
//nolint:cyclop,funlen,gocognit,nestif // Keep each native transaction API visible in this raw probe.
func TestRawTransactions(t *testing.T) {
	ForEach(t, func(t *testing.T, db *Database) {
		ctx := t.Context()

		if db.Backend == SQLite {
			query := "CREATE TABLE probe (id BLOB PRIMARY KEY, value BLOB NOT NULL)"
			if _, err := db.SQLite.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
		} else {
			query := "CREATE TABLE boyan_canonical.probe (id bytea PRIMARY KEY, value bytea NOT NULL)"
			if _, err := db.Postgres.Exec(ctx, query); err != nil {
				t.Fatal(err)
			}
		}

		for _, commit := range []bool{false, true} {
			id := []byte{0, 255, byte(0)}
			if commit {
				id[2] = 1
			}

			if db.Backend == SQLite {
				tx, err := db.SQLite.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}

				if _, err := tx.ExecContext(ctx, "INSERT INTO probe VALUES (?, ?)", id, []byte{1, 0, 255}); err != nil {
					_ = tx.Rollback()

					t.Fatal(err)
				}

				if commit {
					err = tx.Commit()
				} else {
					err = tx.Rollback()
				}

				if err != nil {
					t.Fatal(err)
				}
			} else {
				tx, err := db.Postgres.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}

				if _, err := tx.Exec(ctx, "INSERT INTO boyan_canonical.probe VALUES ($1, $2)", id, []byte{1, 0, 255}); err != nil {
					_ = tx.Rollback(ctx)

					t.Fatal(err)
				}

				if commit {
					err = tx.Commit(ctx)
				} else {
					err = tx.Rollback(ctx)
				}

				if err != nil {
					t.Fatal(err)
				}
			}
		}

		var (
			count int
			err   error
		)
		if db.Backend == SQLite {
			err = db.SQLite.QueryRowContext(ctx, "SELECT count(*) FROM probe").Scan(&count)
		} else {
			err = db.Postgres.QueryRow(ctx, "SELECT count(*) FROM boyan_canonical.probe").Scan(&count)
		}

		if err != nil || count != 1 {
			t.Fatalf("committed rows=%d, want 1; error=%v", count, err)
		}

		var payload []byte
		if db.Backend == SQLite {
			err = db.SQLite.QueryRowContext(ctx, "SELECT value FROM probe WHERE id=?", []byte{0, 255, 1}).Scan(&payload)
		} else {
			row := db.Postgres.QueryRow(ctx, "SELECT value FROM boyan_canonical.probe WHERE id=$1", []byte{0, 255, 1})
			err = row.Scan(&payload)
		}

		if err != nil || !bytes.Equal(payload, []byte{1, 0, 255}) {
			t.Fatalf("committed binary payload differs: error=%v", err)
		}
	})
}

// These are raw engine probes; canonical encoding/schema belong to TASK-002.
//
//nolint:cyclop,funlen,gocognit,nestif // Assert byte ordering through both native row APIs.
func TestRawBinaryUnsignedOrder(t *testing.T) {
	ForEach(t, func(t *testing.T, db *Database) {
		ctx := t.Context()

		query := "CREATE TABLE ordering_probe (id BLOB PRIMARY KEY)"
		if db.Backend == Postgres {
			query = "CREATE TABLE boyan_canonical.ordering_probe (id bytea PRIMARY KEY)"
		}

		if db.Backend == SQLite {
			if _, err := db.SQLite.ExecContext(ctx, query); err != nil {
				t.Fatal(err)
			}
		} else if _, err := db.Postgres.Exec(ctx, query); err != nil {
			t.Fatal(err)
		}

		for _, value := range []uint64{math.MaxUint64, 0, 1 << 63, (1 << 63) - 1} {
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], value)

			if db.Backend == SQLite {
				if _, err := db.SQLite.ExecContext(ctx, "INSERT INTO ordering_probe VALUES (?)", encoded[:]); err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := db.Postgres.Exec(ctx, "INSERT INTO boyan_canonical.ordering_probe VALUES ($1)", encoded[:])
				if err != nil {
					t.Fatal(err)
				}
			}
		}

		var values []uint64

		if db.Backend == SQLite {
			rows, err := db.SQLite.QueryContext(ctx, "SELECT id FROM ordering_probe ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			for rows.Next() {
				var id []byte
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}

				values = append(values, binary.BigEndian.Uint64(id))
			}

			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		} else {
			rows, err := db.Postgres.Query(ctx, "SELECT id FROM boyan_canonical.ordering_probe ORDER BY id")
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()

			for rows.Next() {
				var id []byte
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}

				values = append(values, binary.BigEndian.Uint64(id))
			}

			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
		}

		want := []uint64{0, (1 << 63) - 1, 1 << 63, math.MaxUint64}
		if len(values) != len(want) {
			t.Fatalf("rows=%v", values)
		}

		for i := range want {
			if values[i] != want[i] {
				t.Fatalf("order=%v, want %v", values, want)
			}
		}
	})
}

// Cleanup must not touch the maintenance database or another fixture's objects.
//
//nolint:cyclop,funlen,gocognit // Real sibling, identity mismatch and reconnect share one fixture.
func TestFixtureCleanupIsolation(t *testing.T) {
	ForEach(t, func(t *testing.T, first *Database) {
		ctx := t.Context()

		if first.Backend == SQLite {
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			if err := first.Close(); err != nil {
				t.Fatal(err)
			}

			probe, err := sql.Open("sqlite", first.DSN)
			if err != nil {
				t.Fatal(err)
			}
			defer probe.Close()

			if err := probe.PingContext(ctx); err != nil {
				t.Fatal(err)
			}

			return
		}

		dsn, err := configuredPostgres()
		if err != nil {
			t.Fatal(err)
		}

		second, err := OpenPostgres(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := second.Close(); err != nil {
				t.Error(err)
			}
		}()

		admin, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatal("connect test maintenance database")
		}
		defer admin.Close(ctx)

		var original string
		if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&original); err != nil {
			t.Fatal(err)
		}

		originalOID := first.databaseOID

		reopened, err := pgx.Connect(ctx, first.DSN)
		if err != nil {
			t.Fatal("reopen disposable fixture DSN")
		}

		var reopenedName string

		err = reopened.QueryRow(ctx, "SELECT current_database()").Scan(&reopenedName)
		_ = reopened.Close(ctx)

		if err != nil || reopenedName != first.databaseName {
			t.Fatalf("fixture DSN escaped its disposable database: error=%v", err)
		}

		first.databaseOID = "wrong-identity"
		if err := first.Close(); err == nil {
			t.Fatal("cleanup accepted an unverified database identity")
		}

		first.databaseOID = originalOID
		if err := first.Close(); err != nil {
			t.Fatal(err)
		}

		if err := first.Close(); err != nil {
			t.Fatal(err)
		}

		var count int

		row := admin.QueryRow(ctx, "SELECT count(*) FROM pg_database WHERE datname=$1", first.databaseName)
		if err := row.Scan(&count); err != nil || count != 0 {
			t.Fatalf("fixture database remains: count=%d error=%v", count, err)
		}

		var current string
		if err := admin.QueryRow(ctx, "SELECT current_database()").Scan(&current); err != nil || current != original {
			t.Fatalf("maintenance database changed: %v", err)
		}

		if err := second.Postgres.Ping(ctx); err != nil {
			t.Fatalf("sibling fixture was touched: %v", err)
		}
	})
}

func TestPostgresConfigurationRejectsInvalidRequiredMode(t *testing.T) {
	t.Setenv("BOYAN_REQUIRE_POSTGRES", "sometimes")

	if _, err := configuredPostgres(); err == nil {
		t.Fatal("invalid required mode was accepted")
	}
}

// Connection diagnostics must not expose DSN passwords, even on parse errors.
func TestPostgresConnectionFailureRedactsDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://fixture:secret-marker@127.0.0.1:1/postgres?sslmode=disable&connect_timeout=1",
		"postgres://fixture:secret-marker@localhost/postgres?sslmode=invalid",
	} {
		db, err := OpenPostgres(context.Background(), dsn)
		if db != nil || err == nil {
			t.Fatalf("unavailable configuration: database=%v error=%v", db != nil, err)
		}

		if strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("connection error exposed DSN password")
		}
	}
}
