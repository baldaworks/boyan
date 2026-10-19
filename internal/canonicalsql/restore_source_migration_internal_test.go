package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
)

//nolint:cyclop,gocognit,funlen // Upgrade and rollback retain checksums, fence, canonical data, and mutation clock.
func TestRestoreSourceMigrationPreservesHistoryAndFence(t *testing.T) {
	for _, collide := range []bool{false, true} {
		name := "upgrade"
		if collide {
			name = "atomic-failure"
		}

		t.Run(name, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				c := testConnection(t, db)
				key := Key{Family: Payload, Parts: [][]byte{[]byte("retained")}}
				value := envelope(t, Payload, "retained")
				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, value) })
				// Assemble the previous schema only in this fresh, owned native fixture.
				rawExec(t, c, "DROP TABLE "+c.table("restore_source_capability"))
				rawExec(t, c, "DELETE FROM "+c.table("schema_migration")+" WHERE version=3")
				rawExec(t, c, "INSERT INTO "+c.table("restore_control")+"(singleton,control) VALUES(1,$1)",
					[]byte("retained fence"))

				if collide {
					rawExec(t, c, "CREATE TABLE "+c.table("restore_source_capability")+"(retained INTEGER NOT NULL)")
					rawExec(t, c, "INSERT INTO "+c.table("restore_source_capability")+" VALUES(17)")
				}

				err := c.Migrate(t.Context())
				if (err != nil) != collide {
					t.Fatalf("migration error=%v, collide=%v", err, collide)
				}

				checksums := []string{
					"02c09600117ea5b816f4d590b092cb120035ca597a5aed2e37647dbb6ea82c3e",
					"106d596e5fb497335daba81bab477bf9a924591767014a1db14d73d2975dc6db",
				}
				if c.pg != nil {
					checksums = []string{
						"27d4f5c905d3aa7bdd6fc031c944873a284776a5a916fdf00424c3f03b9f3b4d",
						"7fb98f426885e1e901f07ff9fa6c223ddd360807c2413ab83d0b8a4e463b71a9",
					}
				}

				for i, want := range checksums {
					var got string
					if err := scanOne(t.Context(), c.q, "SELECT checksum FROM "+c.table("schema_migration")+
						" WHERE version=$1", []any{i + 1}, &got); err != nil || got != want {
						t.Fatalf("prior migration changed: %s %v", got, err)
					}
				}

				var version, canonicalVersion int
				if err := scanOne(t.Context(), c.q, "SELECT max(version) FROM "+c.table("schema_migration"),
					nil, &version); err != nil {
					t.Fatal(err)
				}

				wantVersion := 3
				if collide {
					wantVersion = 2
				}

				if err := scanOne(t.Context(), c.q, "SELECT schema_version FROM "+c.table("store_meta"),
					nil, &canonicalVersion); err != nil || version != wantVersion || canonicalVersion != 1 {
					t.Fatalf("migration/canonical versions=%d/%d: %v", version, canonicalVersion, err)
				}

				var fence, payload []byte
				if err := scanOne(t.Context(), c.q, "SELECT control FROM "+c.table("restore_control"), nil, &fence); err != nil {
					t.Fatal(err)
				}

				if err := scanOne(t.Context(), c.q, "SELECT envelope FROM "+c.table("payload"), nil, &payload); err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(payload, value) || string(fence) != "retained fence" {
					t.Fatal("upgrade or its rollback modified data/fence")
				}

				assertClock(t, c, 1, 1)
			})
		})
	}
}
