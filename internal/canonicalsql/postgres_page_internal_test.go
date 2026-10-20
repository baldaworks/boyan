package canonicalsql

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Metadata failure at the last selected key must discard the whole page and
// roll back a preceding write even when the caller swallows the Scan error.
func TestPostgresPageMetadataFailurePoisonsTransaction(t *testing.T) {
	for _, fault := range []string{"duplicate-row", "oversized-row"} {
		t.Run(fault, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			c := testConnection(t, db)
			scope := testScope()

			seedPageSources(t, c, scope)

			want := memory.CodeStoreFailure

			switch fault {
			case "duplicate-row":
				rawExec(t, c, "INSERT INTO "+c.table("source")+
					" (scope_key,scope_kind,id,logical_key,scope_partition,envelope) "+
					"SELECT scope_key,scope_kind,id,logical_key,scope_partition,envelope FROM "+c.table("source")+
					" WHERE id=$1", []byte("b"))
			case "oversized-row":
				want = memory.CodeLimitExceeded

				rawExec(t, c, "ALTER TABLE "+c.table("source")+" DROP CONSTRAINT source_envelope_check")
				rawExec(t, c, "UPDATE "+c.table("source")+" SET envelope=$1 WHERE id=$2",
					make([]byte, MaxEnvelopeBytes+1), []byte("b"))
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
