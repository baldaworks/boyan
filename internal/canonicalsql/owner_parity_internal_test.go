package canonicalsql

import (
	"bytes"
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func forEachOwner(t *testing.T, run func(*testing.T, *Owner, func() *Owner)) {
	t.Helper()
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		if db.SQLite != nil {
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
		}

		open := func() *Owner {
			var (
				owner *Owner
				err   error
			)
			if db.Backend == canonicalsqltest.SQLite {
				owner, err = OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: db.DSN})
			} else {
				owner, err = OpenPostgresOwner(t.Context(), PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
			}

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
		run(t, open(), open)
	})
}

func assertOwnerStillLocked(t *testing.T, owner *Owner) {
	t.Helper()

	var (
		second *Owner
		err    error
	)
	if owner.connection.pg != nil {
		second, err = OpenPostgresOwner(t.Context(), PostgresConfig{
			DSN: owner.connection.pg.Config().ConnString(), AllowLocalInsecure: true,
		})
	} else {
		var (
			sequence   int
			name, path string
		)
		if err := scanOne(t.Context(), owner.connection.q, "PRAGMA database_list", nil, &sequence, &name, &path); err != nil {
			t.Fatal(err)
		}

		second, err = OpenSQLiteOwner(t.Context(), SQLiteConfig{Path: path})
	}

	if second != nil {
		_ = second.Close()

		t.Error("second owner acquired ownership before drain")
	}

	assertCode(t, err, memory.CodeUnavailable)
}

// Concurrent absent-key writers must create one logical identity and stamp.
// Distinct original bytes, including the checked-in native hash collision,
// must remain independent through the actual supported owner and reopening.
func TestOwnerExactIdentityAndDurability(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		keys := []Key{
			{Family: Payload, Parts: [][]byte{[]byte("10187")}},
			{Family: Payload, Parts: [][]byte{[]byte("159793")}},
			{Family: Payload, Parts: [][]byte{[]byte("10187\x00")}},
		}
		value := envelope(t, Payload, "durable")
		start, result := make(chan struct{}), make(chan error, 2)

		for range 2 {
			go func() {
				<-start

				result <- owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					return tx.Put(ctx, keys[0], value)
				})
			}()
		}

		close(start)

		for range 2 {
			if err := <-result; err != nil {
				t.Fatal(err)
			}
		}

		if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			for _, key := range keys[1:] {
				if err := tx.Put(ctx, key, value); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		if err := owner.Close(); err != nil {
			t.Fatal(err)
		}

		owner = reopen()
		if err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			assertOwnerRows(t, ctx, tx, keys, value)

			return tx.CheckIntegrity(ctx)
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func assertOwnerRows(t *testing.T, ctx context.Context, tx *Tx, keys []Key, value []byte) {
	t.Helper()

	if tx.connection.pg != nil {
		var pid uint32
		if err := scanOne(ctx, tx.q, "SELECT pg_catalog.pg_backend_pid()", nil, &pid); err != nil ||
			pid != tx.connection.pg.PgConn().PID() {
			t.Fatalf("transaction detached from lock-owning session: %v", err)
		}
	}

	for _, key := range keys {
		got, err := tx.Get(ctx, key)
		if err != nil || !bytes.Equal(got, value) {
			t.Fatalf("lost independent identity on reopen: %v", err)
		}
	}
}
