package canonicalsql

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRepairDoesNotRewriteSQLiteDDL(t *testing.T) {
	digest := sha256.Sum256([]byte(sqliteDDL))
	if hex.EncodeToString(digest[:]) != "02c09600117ea5b816f4d590b092cb120035ca597a5aed2e37647dbb6ea82c3e" {
		t.Fatal("SQLite physical schema changed")
	}
}

func TestUnsupportedScaffoldAndFuturePreserveRestoreFence(t *testing.T) {
	for _, future := range []bool{false, true} {
		name := "old_unshipped_checksum"
		if future {
			name = "future"
		}

		t.Run(name, func(t *testing.T) {
			canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
				c := testConnection(t, db)
				key := Key{Family: Payload, Parts: [][]byte{[]byte("preserve")}}
				encoded := envelope(t, Payload, true)
				mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })
				rawExec(t, c, "INSERT INTO "+c.table("restore_control")+"(singleton,control) VALUES(1,$1)",
					[]byte("unfinished restore"))

				if future {
					rawExec(t, c, "INSERT INTO "+c.table("schema_migration")+"(version,checksum) VALUES(2,'future')")
				} else {
					rawExec(t, c, "UPDATE "+c.table("schema_migration")+" SET checksum=$1",
						"d2c06b7a96a650818c8829158f519be2cfe4480a73af8ed836d658f1d09ba519")
				}

				assertCode(t, c.Migrate(t.Context()), memory.CodeInvalidDerived)

				var value, control []byte
				if err := scanOne(t.Context(), c.q, "SELECT envelope FROM "+c.table("payload")+" WHERE id=$1",
					[]any{key.Parts[0]}, &value); err != nil {
					t.Fatal(err)
				}

				if err := scanOne(t.Context(), c.q, "SELECT control FROM "+c.table("restore_control"), nil, &control); err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(value, encoded) || string(control) != "unfinished restore" {
					t.Fatal("rejection modified durable state/fence")
				}

				assertClock(t, c, 1, 1)
			})
		})
	}
}
