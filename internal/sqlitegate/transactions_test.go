//go:build integration

package sqlitegate_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"
)

// DSN initialization must run on each physical connection, including two held
// concurrently; checking only a pool's first connection would miss this bug.
func TestPhysicalConnectionPragmasAndPrivateWAL(t *testing.T) {
	t.Parallel()
	writer, reader, path := openFixture(t)
	execFixture(t, writer, `CREATE TABLE state(id INTEGER PRIMARY KEY,value INTEGER)`)
	execFixture(t, writer, `INSERT INTO state VALUES(1,1)`)

	for _, pool := range []struct {
		db              *sql.DB
		size, queryOnly int
	}{{writer, 1, 0}, {reader, 2, 1}} {
		var connections []*sql.Conn

		for i := 0; i < pool.size; i++ {
			conn, err := pool.db.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			connections = append(connections, conn)
		}

		for _, conn := range connections {
			assertConnectionSettings(t, conn, pool.queryOnly)

			if pool.queryOnly == 1 {
				if _, err := conn.ExecContext(t.Context(), `INSERT INTO state VALUES(2,2)`); err == nil {
					t.Fatal("query_only reader accepted write")
				}
			}
		}

		for _, conn := range connections {
			if err := conn.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}

	assertPrivateReopen(t, path)
}

func assertPrivateReopen(t *testing.T, path string) {
	t.Helper()

	for _, name := range []string{path, path + "-wal", path + "-shm"} {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}

		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode=%o", name, info.Mode().Perm())
		}
	}
	// Reopen while WAL is live and check the same persisted data/settings.
	reopened := fixturePool(t, path, true, 1)

	var value int
	if err := reopened.QueryRowContext(t.Context(), "SELECT value FROM state").Scan(&value); err != nil {
		t.Fatal(err)
	}

	if value != 1 {
		t.Fatalf("reopen value=%d", value)
	}
}

func assertConnectionSettings(t *testing.T, conn *sql.Conn, queryOnly int) {
	t.Helper()

	for _, setting := range []struct {
		name string
		want int
	}{
		{"synchronous", 2}, {"foreign_keys", 1}, {"busy_timeout", 1000}, {"temp_store", 2}, {"query_only", queryOnly},
	} {
		var got int
		if err := conn.QueryRowContext(t.Context(), "PRAGMA "+setting.name).Scan(&got); err != nil {
			t.Fatal(err)
		}

		if got != setting.want {
			t.Fatalf("%s=%d want=%d", setting.name, got, setting.want)
		}
	}

	var mode, version string
	if err := conn.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}

	if mode != "wal" {
		t.Fatalf("mode=%s", mode)
	}

	if err := conn.QueryRowContext(t.Context(), "SELECT vec_version()").Scan(&version); err != nil {
		t.Fatal(err)
	}

	if version != "v0.1.9" {
		t.Fatalf("vec=%s", version)
	}
}

// Both channels in one transaction must retain their old view while a writer
// publishes both atomically; failure/cancellation must not leak partial state.
func TestSharedSnapshotRollbackAndCancellation(t *testing.T) {
	t.Parallel()
	writer, reader, _ := openFixture(t)
	execFixture(t, writer, `CREATE TABLE channels(lexical INTEGER,semantic INTEGER CHECK(semantic>=0))`)
	execFixture(t, writer, `INSERT INTO channels VALUES(1,1)`)

	snapshot, err := reader.BeginTx(t.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = snapshot.Rollback() })
	assertPair(t, snapshot, 1, 1)
	execFixture(t, writer, `UPDATE channels SET lexical=2,semantic=2`)
	assertPair(t, snapshot, 1, 1)

	if err := snapshot.Commit(); err != nil {
		t.Fatal(err)
	}

	assertPair(t, reader, 2, 2)

	tx, err := writer.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	execFixture(t, tx, `UPDATE channels SET lexical=3`)

	if _, err := tx.ExecContext(t.Context(), `UPDATE channels SET semantic=-1`); err == nil {
		t.Fatal("constraint failure expected")
	}

	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	assertPair(t, reader, 2, 2)
}

func TestTransactionAndScanCancellation(t *testing.T) {
	t.Parallel()
	writer, reader, _ := openFixture(t)
	execFixture(t, writer, `CREATE TABLE channels(lexical INTEGER,semantic INTEGER)`)
	execFixture(t, writer, `INSERT INTO channels VALUES(2,2)`)
	ctx, cancel := context.WithCancel(t.Context())

	tx, err := writer.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	execFixture(t, tx, `UPDATE channels SET lexical=4,semantic=4`)
	cancel()

	if err := tx.Commit(); err == nil {
		t.Fatal("canceled transaction committed")
	}

	assertPair(t, reader, 2, 2)
	// Cancel work that has actually started stepping, rather than only handing
	// the driver an already-canceled context.
	ctx, cancel = context.WithCancel(t.Context())

	rows, err := reader.QueryContext(ctx, `WITH RECURSIVE n(x) AS (
	 VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1000000000) SELECT x FROM n`)
	if err != nil {
		cancel()
		t.Fatal(err)
	}

	if !rows.Next() {
		cancel()
		t.Fatal("scan did not begin")
	}

	cancel()

	for rows.Next() {
	}

	if !errors.Is(rows.Err(), context.Canceled) {
		t.Fatalf("scan cancellation=%v", rows.Err())
	}

	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}

	assertPair(t, reader, 2, 2)
}

func TestQueuedWriterCancellation(t *testing.T) {
	t.Parallel()
	writer, reader, _ := openFixture(t)
	execFixture(t, writer, `CREATE TABLE channels(lexical INTEGER,semantic INTEGER)`)
	execFixture(t, writer, `INSERT INTO channels VALUES(2,2)`)
	// Waiting for the single writer connection is also context bounded.
	conn, err := writer.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	_, err = writer.ExecContext(ctx, `UPDATE channels SET lexical=5`)

	cancel()

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued writer cancellation=%v", err)
	}

	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	assertPair(t, reader, 2, 2)
}

func assertPair(t *testing.T, db interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}, wantLexical, wantSemantic int) {
	t.Helper()

	var lexical, semantic int

	err := db.QueryRowContext(t.Context(), `SELECT lexical,semantic FROM channels`).Scan(&lexical, &semantic)
	if err != nil {
		t.Fatal(err)
	}

	if lexical != wantLexical || semantic != wantSemantic {
		t.Fatalf("pair=(%d,%d),want=(%d,%d)", lexical, semantic, wantLexical, wantSemantic)
	}
}
