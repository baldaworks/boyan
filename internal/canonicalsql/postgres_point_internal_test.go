package canonicalsql

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A missing row cannot hide a corrupt surviving stamp, including a tombstone.
// Swallowing a read or write failure must not commit an earlier valid write.
// An oversized physical envelope retains its limit error even when the stamp
// is also corrupt. A combined probe must not decode the stamp first.
func TestPostgresRowLimitPrecedesStampCorruption(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	c := testConnection(t, db)
	key := Key{Family: Payload, Parts: [][]byte{[]byte("oversized")}}

	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Payload, "value")) })
	rawExec(t, c, "ALTER TABLE "+c.table("payload")+" DROP CONSTRAINT payload_envelope_check")
	rawExec(t, c, "UPDATE "+c.table("payload")+" SET envelope=$1", make([]byte, MaxEnvelopeBytes+1))

	err := c.Update(t.Context(), func(tx *Tx) error {
		_, _ = tx.Get(context.Background(), key)

		return nil
	})
	assertCode(t, err, memory.CodeLimitExceeded)
}

// A late related-row failure must discard the already decoded first item and
// poison the admitted writer even if its caller swallows the read error.
func TestPostgresActiveRelatedFailureRollsBackWholePage(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
	scope := testScope()
	seedActivePage(t, owner, scope, []string{"first", "second"})
	c := owner.connection
	rawExec(t, c, "UPDATE "+c.table("item")+" SET envelope=$1 WHERE id=$2", []byte("bad"), []byte("second"))

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

	var payloads int
	if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("payload"), nil, &payloads); err != nil {
		t.Fatal(err)
	}

	if payloads != 0 {
		t.Fatal("swallowed stream failure committed a payload")
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

			page, err := owner.ScanActiveMemory(t.Context(), memory.ActiveMemoryScanRequest{Scope: scope, Limit: 2})
			assertCode(t, err, want)

			if page != nil {
				t.Fatal("failed ordered stream exposed data")
			}
		})
	}
}
