package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A future migration must be rejected without changing live canonical data.
func TestMigrationRejectsFutureWithoutWrites(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Payload, Parts: [][]byte{[]byte("preserve")}}
		encoded := envelope(t, Payload, "unchanged")
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })
		rawExec(t, c, "INSERT INTO "+c.table("schema_migration")+" (version,checksum) VALUES(4,'future')")
		assertCode(t, c.Migrate(t.Context()), memory.CodeInvalidDerived)

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

		for _, table := range []string{"schema_migration", "source"} {
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
