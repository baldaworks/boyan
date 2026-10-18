package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Catches repairing unknown/checksum-modified history or mutating live state
// while rejecting incompatible migrations.
func TestMigrationRejectsFutureAndChecksumWithoutWrites(t *testing.T) {
	for _, fault := range []string{"checksum", "future"} {
		t.Run(fault, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				c := testConnection(t, db)
				key := Key{Family: Payload, Parts: [][]byte{[]byte("preserve")}}
				encoded := envelope(t, Payload, "unchanged")
				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })

				var before []byte
				if err := scanOne(t.Context(), c.q, "SELECT store_uuid FROM "+c.table("store_meta"), nil, &before); err != nil {
					t.Fatal(err)
				}

				if fault == "checksum" {
					rawExec(t, c, "UPDATE "+c.table("schema_migration")+" SET checksum='wrong'")
				} else {
					rawExec(t, c, "INSERT INTO "+c.table("schema_migration")+" (version,checksum) VALUES(3,'future')")
				}

				assertCode(t, c.Migrate(t.Context()), memory.CodeInvalidDerived)
				assertClock(t, c, 1, 1)

				var after []byte
				if err := scanOne(t.Context(), c.q, "SELECT store_uuid FROM "+c.table("store_meta"), nil, &after); err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(before, after) {
					t.Fatal("replaced store lineage")
				}

				if err := c.View(t.Context(), func(tx *Tx) error {
					got, err := tx.Get(t.Context(), key)
					if !bytes.Equal(got, encoded) {
						t.Fatal("migration rejection modified live row")
					}

					return err
				}); err != nil {
					t.Fatal(err)
				}
			})
		})
	}
}

// Catches DDL escaping the migration transaction after a later statement fails.
func TestMigrationDDLFailureIsAtomic(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		var c *Connection
		if db.SQLite != nil {
			c = NewSQLite(db.SQLite)
		} else {
			c = NewPostgres(db.Postgres)
		}

		rawExec(t, c, "CREATE TABLE "+c.table("item")+" (existing INTEGER NOT NULL)")
		rawExec(t, c, "INSERT INTO "+c.table("item")+" VALUES(17)")

		if err := c.Migrate(t.Context()); err == nil {
			t.Fatal("colliding schema accepted")
		}

		var value int
		if err := scanOne(t.Context(), c.q, "SELECT existing FROM "+c.table("item"), nil, &value); err != nil || value != 17 {
			t.Fatal("prior object changed")
		}

		for _, table := range []string{"store_meta", "schema_migration", "source"} {
			var count int

			query := "SELECT count(*) FROM sqlite_master WHERE type='table' AND name=$1"
			if db.Postgres != nil {
				query = "SELECT count(*) FROM information_schema.tables WHERE table_schema='boyan_canonical' AND table_name=$1"
			}

			if err := scanOne(t.Context(), c.q, query, []any{table}, &count); err != nil {
				t.Fatal(err)
			}

			if count != 0 {
				t.Fatalf("migration partially created %s", table)
			}
		}
	})
}

// Catches ordinary reads/writes bypassing the durable, non-archive control row.
func TestRestoreControlFencesReadsAndWrites(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		rawExec(t, c, "INSERT INTO "+c.table("restore_control")+" (singleton,control) VALUES(1,$1)", []byte("loading"))

		called := false
		callback := func(*Tx) error {
			called = true

			return nil
		}
		assertCode(t, c.View(t.Context(), callback), memory.CodeUnavailable)
		assertCode(t, c.Update(t.Context(), callback), memory.CodeUnavailable)

		if called {
			t.Fatal("fenced callback executed")
		}

		assertClock(t, c, 0, 0)

		if _, err := familyDefinition(Family("restore_control")); err == nil {
			t.Fatal("control entered family dispatch")
		}

		if err := c.Migrate(t.Context()); err != nil {
			t.Fatal(err)
		}

		var control []byte
		if err := scanOne(t.Context(), c.q, "SELECT control FROM "+c.table("restore_control"), nil, &control); err != nil {
			t.Fatal(err)
		}

		if string(control) != "loading" {
			t.Fatal("migration cleared restore fence")
		}
	})
}
