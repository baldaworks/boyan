package canonicalsql

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Metadata failure at the last selected key must discard the whole page and
// roll back a preceding write even when the caller swallows the Scan error.
func TestPostgresPageMetadataFailurePoisonsTransaction(t *testing.T) {
	for _, fault := range []string{"duplicate-row", "duplicate-stamp", "invalid-stamp", "physical-before-metadata"} {
		t.Run(fault, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			c := testConnection(t, db)
			scope := testScope()

			seedPageSources(t, c, scope)

			want, stamps := memory.CodeStoreFailure, 2

			switch fault {
			case "duplicate-row":
				rawExec(t, c, "INSERT INTO "+c.table("source")+
					" (scope_key,scope_kind,id,logical_key,scope_partition,envelope) "+
					"SELECT scope_key,scope_kind,id,logical_key,scope_partition,envelope FROM "+c.table("source")+
					" WHERE id=$1", []byte("b"))
			case "duplicate-stamp":
				stamps++

				rawExec(t, c, "INSERT INTO "+c.table("row_stamp")+" (family,binary_key,last_seq,deleted) "+
					"SELECT family,binary_key,last_seq,deleted FROM "+c.table("row_stamp")+
					" WHERE binary_key=$1", stampKey([][]byte{[]byte(scope.Key), []byte(scope.Kind), []byte("b")}))
			default:
				rawExec(t, c, "ALTER TABLE "+c.table("row_stamp")+" DROP CONSTRAINT row_stamp_last_seq_check")
				rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET last_seq=$1", []byte{1})

				if fault == "physical-before-metadata" {
					want = memory.CodeLimitExceeded

					rawExec(t, c, "ALTER TABLE "+c.table("source")+" DROP CONSTRAINT source_envelope_check")
					rawExec(t, c, "UPDATE "+c.table("source")+" SET envelope=$1 WHERE id=$2",
						make([]byte, MaxEnvelopeBytes+1), []byte("b"))
				}
			}

			err := c.Update(t.Context(), func(tx *Tx) error {
				key := Key{Family: Payload, Parts: [][]byte{[]byte("rollback")}}
				if err := tx.Put(t.Context(), key, envelope(t, Payload, "uncommitted")); err != nil {
					return err
				}

				page, err := tx.Scan(t.Context(), Range{Family: Source, Scope: &scope, Limit: 2})
				assertCode(t, err, want)

				if page != nil {
					t.Fatal("page failure leaked selected rows")
				}

				return nil
			})
			assertCode(t, err, want)
			assertClock(t, c, 1, stamps)
			assertPagePayloadRollback(t, c)
		})
	}
}

func seedPageSources(t *testing.T, c *Connection, scope memory.Scope) {
	t.Helper()

	mustUpdate(t, c, func(tx *Tx) error {
		for _, id := range []string{"a", "b"} {
			key := Key{Family: Source, Scope: scope, Parts: [][]byte{[]byte(id)}}
			if err := tx.Put(t.Context(), key, envelope(t, Source, id)); err != nil {
				return err
			}
		}

		return nil
	})
}

func assertPagePayloadRollback(t *testing.T, c *Connection) {
	t.Helper()

	var payloads int
	if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("payload"), nil, &payloads); err != nil {
		t.Fatal(err)
	}

	if payloads != 0 {
		t.Fatal("swallowed page failure committed prior write")
	}
}

// Ordinary reads do not require a stamp to exist; integrity checks own that
// stronger invariant. Batching must not silently tighten the read contract.
func TestPostgresPageAllowsAbsentStamp(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	c := testConnection(t, db)
	key := Key{Family: Payload, Parts: [][]byte{[]byte("visible")}}

	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Payload, "value")) })
	rawExec(t, c, "DELETE FROM "+c.table("row_stamp"))

	if err := c.View(t.Context(), func(tx *Tx) error {
		page, err := tx.Scan(t.Context(), Range{Family: Payload, Limit: 1})
		if err != nil {
			return err
		}

		if len(page) != 1 || string(page[0].Key.Parts[0]) != "visible" {
			t.Fatal("absent stamp hid selected row")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
