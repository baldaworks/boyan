//go:build integration

package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Simulated prior migration history uses the actual previous initial checksum.
// Refusal must leave schema history and actual canonical data unchanged.
//nolint:cyclop // One native fixture verifies refusal and each independent unchanged-state assertion.
func TestPriorSchemaChecksumRefusalPreservesState(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		checksum := "02c09600117ea5b816f4d590b092cb120035ca597a5aed2e37647dbb6ea82c3e"

		c := testConnection(t, db)
		if db.Postgres != nil {
			checksum = "27d4f5c905d3aa7bdd6fc031c944873a284776a5a916fdf00424c3f03b9f3b4d"
		}

		key := Key{Family: Payload, Parts: [][]byte{[]byte("prior-data")}}
		encoded := envelope(t, Payload, "untouched canonical bytes")
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })
		rawExec(t, c, "UPDATE "+c.table("schema_migration")+" SET checksum=$1 WHERE version=1", checksum)
		rawExec(t, c, "CREATE TABLE "+c.table("fixture_user_state")+" (value TEXT NOT NULL)")
		rawExec(t, c, "INSERT INTO "+c.table("fixture_user_state")+" VALUES('untouched user state')")
		before := priorSchemaTableCount(t, c)
		assertCode(t, c.Migrate(t.Context()), memory.CodeInvalidDerived)

		if after := priorSchemaTableCount(t, c); after != before {
			t.Fatalf("schema tables changed: %d -> %d", before, after)
		}

		var storedChecksum, value string

		var history int

		if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("schema_migration"), nil, &history); err != nil {
			t.Fatal(err)
		}

		if err := scanOne(t.Context(), c.q, "SELECT checksum FROM "+c.table("schema_migration")+
			" WHERE version=1", nil, &storedChecksum); err != nil {
			t.Fatal(err)
		}

		if err := scanOne(t.Context(), c.q, "SELECT value FROM "+c.table("fixture_user_state"), nil, &value); err != nil {
			t.Fatal(err)
		}

		if history != 1 || storedChecksum != checksum || value != "untouched user state" {
			t.Fatal("schema refusal modified prior state")
		}

		if err := c.View(t.Context(), func(tx *Tx) error {
			got, err := tx.Get(t.Context(), key)
			if !bytes.Equal(got, encoded) {
				t.Fatal("schema refusal changed canonical bytes")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func priorSchemaTableCount(t *testing.T, c *Connection) int {
	t.Helper()

	statement := "SELECT count(*) FROM sqlite_master WHERE type='table'"
	if c.pg != nil {
		statement = "SELECT count(*) FROM information_schema.tables WHERE table_schema='boyan_canonical'"
	}

	var count int

	if err := scanOne(t.Context(), c.q, statement, nil, &count); err != nil {
		t.Fatal(err)
	}

	return count
}
