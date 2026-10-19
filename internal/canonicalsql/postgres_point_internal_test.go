package canonicalsql

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A missing row cannot hide a corrupt surviving stamp, including a tombstone.
// Swallowing a read or write failure must not commit an earlier valid write.
func TestPostgresMissingRowRejectsCorruptStamp(t *testing.T) {
	for _, fault := range []string{"invalid-sequence", "duplicate"} {
		t.Run(fault, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			c := testConnection(t, db)
			key := Key{Family: Payload, Parts: [][]byte{[]byte("erased")}}

			mustUpdate(t, c, func(tx *Tx) error {
				return tx.Delete(t.Context(), key)
			})

			if fault == "invalid-sequence" {
				rawExec(t, c, "ALTER TABLE "+c.table("row_stamp")+" DROP CONSTRAINT row_stamp_last_seq_check")
				rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET last_seq=$1", []byte{1})
			} else {
				rawExec(t, c, "INSERT INTO "+c.table("row_stamp")+" (family,binary_key,last_seq,deleted) "+
					"SELECT family,binary_key,last_seq,deleted FROM "+c.table("row_stamp"))
			}

			for _, operation := range []string{"get", "put", "delete"} {
				t.Run(operation, func(t *testing.T) {
					err := c.Update(t.Context(), func(tx *Tx) error {
						other := Key{Family: Payload, Parts: [][]byte{[]byte("must-rollback")}}
						if err := tx.Put(t.Context(), other, envelope(t, Payload, "uncommitted")); err != nil {
							return err
						}

						switch operation {
						case "get":
							_, _ = tx.Get(t.Context(), key)
						case "put":
							_ = tx.Put(t.Context(), key, envelope(t, Payload, "replacement"))
						case "delete":
							_ = tx.Delete(t.Context(), key)
						}

						return nil
					})
					assertCode(t, err, memory.CodeStoreFailure)
					assertPointCorruptionRollback(t, c, fault)
				})
			}
		})
	}
}

func assertPointCorruptionRollback(t *testing.T, c *Connection, fault string) {
	t.Helper()

	stamps := 1
	if fault == "duplicate" {
		stamps = 2
	}

	assertClock(t, c, 1, stamps)

	var count int
	if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("payload"), nil, &count); err != nil {
		t.Fatal(err)
	}

	if count != 0 {
		t.Fatal("swallowed corruption committed a payload")
	}
}

// An oversized physical envelope retains its limit error even when the stamp
// is also corrupt. A combined probe must not decode the stamp first.
func TestPostgresRowLimitPrecedesStampCorruption(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	c := testConnection(t, db)
	key := Key{Family: Payload, Parts: [][]byte{[]byte("oversized")}}

	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Payload, "value")) })
	rawExec(t, c, "ALTER TABLE "+c.table("payload")+" DROP CONSTRAINT payload_envelope_check")
	rawExec(t, c, "UPDATE "+c.table("payload")+" SET envelope=$1", make([]byte, MaxEnvelopeBytes+1))
	rawExec(t, c, "ALTER TABLE "+c.table("row_stamp")+" DROP CONSTRAINT row_stamp_last_seq_check")
	rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET last_seq=$1", []byte{1})
	err := c.Update(t.Context(), func(tx *Tx) error {
		_, _ = tx.Get(context.Background(), key)

		return nil
	})
	assertCode(t, err, memory.CodeLimitExceeded)
	assertClock(t, c, 1, 1)
}

// A late related-row failure must discard the already decoded first item and
// poison the admitted writer even if its caller swallows the read error.
func TestPostgresActiveRelatedFailureRollsBackWholePage(t *testing.T) {
	for _, fault := range []string{"late-item", "missing-item-bad-stamp"} {
		t.Run(fault, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
			scope := testScope()
			seedActivePage(t, owner, scope, []string{"first", "second"})
			c := owner.connection

			if fault == "late-item" {
				rawExec(t, c, "UPDATE "+c.table("item")+" SET envelope=$1 WHERE id=$2", []byte("bad"), []byte("second"))
			} else {
				rawExec(t, c, "DELETE FROM "+c.table("item")+" WHERE id=$1", []byte("second"))
				rawExec(t, c, "ALTER TABLE "+c.table("row_stamp")+" DROP CONSTRAINT row_stamp_last_seq_check")

				key, _ := privacyKey(scope, Item, "second")
				_, values, _ := key.values()
				rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET last_seq=$1 WHERE family=$2 AND binary_key=$3",
					[]byte{1}, []byte(Item), stampKey(values))
			}

			err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
				key := Key{Family: Payload, Parts: [][]byte{[]byte("rollback")}}
				if err := tx.Put(ctx, key, envelope(t, Payload, "must roll back")); err != nil {
					return err
				}

				page, err := scanActiveMemoryTx(ctx, tx, memory.ActiveMemoryScanRequest{Scope: scope, Limit: 2})
				assertCode(t, err, memory.CodeStoreFailure)

				if page != nil {
					t.Fatal("late corruption returned a partial page")
				}

				return nil
			})
			assertCode(t, err, memory.CodeStoreFailure)
			assertClock(t, c, 1, 6)

			var payloads int
			if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("payload"), nil, &payloads); err != nil {
				t.Fatal(err)
			}

			if payloads != 0 {
				t.Fatal("swallowed stream failure committed a payload")
			}
		})
	}
}

func TestPostgresActiveRelatedPreservesErrorOrder(t *testing.T) {
	for _, first := range []bool{true, false} {
		name, oversized, want := "item-before-revision-and-stamp", "first", memory.CodeLimitExceeded
		if !first {
			name, oversized, want = "head-order", "second", memory.CodeStoreFailure
		}

		t.Run(name, func(t *testing.T) {
			db := canonicalsqltest.PostgresDatabase(t)
			owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
			scope := testScope()
			seedActivePage(t, owner, scope, []string{"first", "second"})
			c := owner.connection
			rawExec(t, c, "ALTER TABLE "+c.table("item")+" DROP CONSTRAINT item_envelope_check")
			rawExec(t, c, "UPDATE "+c.table("item")+" SET envelope=$1 WHERE id=$2",
				make([]byte, MaxEnvelopeBytes+1), []byte(oversized))
			rawExec(t, c, "UPDATE "+c.table("revision")+" SET envelope=$1 WHERE id=$2", []byte("bad"), []byte("first"))

			if first {
				rawExec(t, c, "ALTER TABLE "+c.table("row_stamp")+" DROP CONSTRAINT row_stamp_last_seq_check")

				key, _ := privacyKey(scope, Item, "first")
				_, values, _ := key.values()
				rawExec(t, c, "UPDATE "+c.table("row_stamp")+" SET last_seq=$1 WHERE family=$2 AND binary_key=$3",
					[]byte{1}, []byte(Item), stampKey(values))
			}

			page, err := owner.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope, Limit: 2})
			assertCode(t, err, want)

			if page != nil {
				t.Fatal("failed ordered stream exposed data")
			}
		})
	}
}
