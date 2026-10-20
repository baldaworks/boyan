package canonicalsql

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Terminate only the known owner PID in this helper-owned child database.
// The maintenance connection and unrelated sessions can never match this SQL.
func terminateTestOwner(t *testing.T, db *canonicalsqltest.Database, pid uint32) {
	t.Helper()

	var terminated bool

	err := db.Postgres.QueryRow(t.Context(), `SELECT pg_catalog.pg_terminate_backend(pid)
		FROM pg_catalog.pg_stat_activity WHERE pid=$1 AND datname=pg_catalog.current_database()
		AND usename=current_user AND pid<>pg_catalog.pg_backend_pid()`, pid).Scan(&terminated)
	if err != nil || !terminated {
		t.Fatalf("could not terminate exact fixture owner: %v", err)
	}
}

//nolint:cyclop // Each native disconnect phase verifies rejection, reopen and absent uncommitted data.
func TestPostgresOwnerDisconnectBoundaries(t *testing.T) {
	for _, phase := range []string{"read", "transaction", "commit_boundary"} {
		t.Run(phase, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			config := PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true}
			owner := openPostgresTestOwner(t, config)
			pid := owner.connection.pg.PgConn().PID()
			key := Key{Family: Payload, Parts: [][]byte{[]byte("unacknowledged")}}
			calls := 0

			err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				calls++

				if phase != "read" {
					if err := tx.Put(ctx, key, envelope(t, Payload, "uncommitted")); err != nil {
						return err
					}
				}

				terminateTestOwner(t, db, pid)

				if phase == "commit_boundary" {
					return nil // The next native operation is COMMIT, never a retry.
				}

				_, err := tx.Get(ctx, key)

				return err
			})
			if err == nil || calls != 1 {
				t.Fatalf("lost connection acknowledged or retried: calls=%d err=%v", calls, err)
			}

			assertPoisonedOwner(t, owner)

			if err := owner.Close(); err != nil {
				t.Fatal(err)
			}

			reopened := openPostgresTestOwner(t, config)
			if reopened.connection.pg.PgConn().PID() == pid {
				t.Fatal("explicit reopen reused terminated owner")
			}

			if err := reopened.CheckIntegrity(t.Context()); err != nil {
				t.Fatal(err)
			}

			if err := reopened.View(t.Context(), func(ctx context.Context, tx *Tx) error {
				_, err := tx.Get(ctx, key)
				assertCode(t, err, memory.CodeNotFound)

				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertPoisonedOwner(t *testing.T, owner *Owner) {
	t.Helper()
	assertCode(t, owner.CheckIntegrity(t.Context()), memory.CodeUnavailable)
	err := owner.Update(t.Context(), func(context.Context, *Tx) error {
		t.Error("disconnected owner admitted subsequent write")

		return nil
	})
	assertCode(t, err, memory.CodeUnavailable)
}

func TestPostgresOwnerProtocolFailure(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
	// An invalid frontend message reaches the real PostgreSQL protocol parser.
	// Only this owner socket is touched; pgx must not reconnect after FATAL.
	if _, err := owner.connection.pg.PgConn().Conn().Write([]byte{'?', 0, 0, 0, 4}); err != nil {
		t.Fatal("write protocol fault to owned session")
	}

	if err := owner.CheckIntegrity(t.Context()); err == nil {
		t.Fatal("protocol failure retained readiness")
	}

	assertPoisonedOwner(t, owner)
}

func TestPostgresOwnerTerminationDuringRead(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
	pid := owner.connection.pg.PgConn().PID()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- owner.View(ctx, func(admitted context.Context, tx *Tx) error {
			_, err := tx.q.exec(admitted, "SELECT pg_catalog.pg_sleep(30)")

			return databaseError(admitted, "test blocked read", err)
		})
	}()

	awaitOwnerSleeping(t, ctx, db, pid)
	terminateTestOwner(t, db, pid)

	if err := <-result; err == nil {
		t.Fatal("interrupted read acknowledged")
	}

	assertPoisonedOwner(t, owner)
}

func awaitOwnerSleeping(t *testing.T, ctx context.Context, db *canonicalsqltest.Database, pid uint32) {
	t.Helper()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		var sleeping bool

		err := db.Postgres.QueryRow(ctx, `SELECT wait_event='PgSleep' FROM pg_catalog.pg_stat_activity
			WHERE pid=$1 AND datname=current_database()`, pid).Scan(&sleeping)
		if err == nil && sleeping {
			return
		}

		select {
		case <-ctx.Done():
			t.Fatal("owned read never reached blocking query")
		case <-ticker.C:
		}
	}
}

// A deferred trigger makes the real server block inside COMMIT. Termination
// then loses the commit response, and must never cause an automatic retry.
func TestPostgresOwnerTerminationDuringCommit(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	config := PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true}
	owner := openPostgresTestOwner(t, config)

	_, err := db.Postgres.Exec(t.Context(), `
		CREATE FUNCTION boyan_canonical.test_commit_fault() RETURNS trigger LANGUAGE plpgsql AS
		'BEGIN PERFORM pg_catalog.pg_sleep(30); RETURN NEW; END';
		CREATE CONSTRAINT TRIGGER test_commit_fault AFTER INSERT ON boyan_canonical.payload
		DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION boyan_canonical.test_commit_fault()`)
	if err != nil {
		t.Fatal("install deferred fault on owned fixture")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	result := make(chan error, 1)
	go func() {
		result <- owner.Update(ctx, func(admitted context.Context, tx *Tx) error {
			return tx.Put(admitted, Key{Family: Payload, Parts: [][]byte{[]byte("commit-fault")}},
				envelope(t, Payload, "uncommitted"))
		})
	}()

	pid := owner.connection.pg.PgConn().PID()
	awaitOwnerSleeping(t, ctx, db, pid)
	terminateTestOwner(t, db, pid)

	if err := <-result; err == nil {
		t.Fatal("in-flight commit loss acknowledged")
	}

	assertPoisonedOwner(t, owner)

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openPostgresTestOwner(t, config)
	if err := reopened.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		_, err := tx.Get(ctx, Key{Family: Payload, Parts: [][]byte{[]byte("commit-fault")}})
		assertCode(t, err, memory.CodeNotFound)

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresOwnerNativeQueryCancellation(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()

	err := owner.View(ctx, func(admitted context.Context, tx *Tx) error {
		_, err := tx.q.exec(admitted, "SELECT pg_catalog.pg_sleep(30)")

		return databaseError(admitted, "test canceled read", err)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("native cancellation lost context identity: %v", err)
	}

	assertPoisonedOwner(t, owner)
}
