//go:build integration

package canonicalsql

import (
	"bytes"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestOversizedStoredEnvelopeFailsClosed(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		key := Key{Family: Source, Scope: scope, Parts: [][]byte{[]byte("id")}}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })

		if db.SQLite != nil {
			rawExec(t, c, "PRAGMA ignore_check_constraints=ON")
		} else {
			rawExec(t, c, "ALTER TABLE boyan_canonical.source DROP CONSTRAINT source_envelope_check")
		}

		rawExec(t, c, "UPDATE "+c.table("source")+" SET envelope=$1", bytes.Repeat([]byte{1}, MaxEnvelopeBytes+1))

		if db.SQLite != nil {
			rawExec(t, c, "PRAGMA ignore_check_constraints=OFF")
		}

		err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)

			return err
		})
		assertCode(t, err, memory.CodeLimitExceeded)
		err = c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Scan(t.Context(), Range{Family: Source, Scope: &scope, Limit: 1})

			return err
		})
		assertCode(t, err, memory.CodeLimitExceeded)
	})
}

// Catches read paths returning corrupt bytes, key/column binding drift, and
// malformed writes advancing clocks. Both direct get and range reads validate.
func TestCorruptEnvelopeAndMaterializedColumnsFailClosed(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		key := Key{Family: Message, Scope: scope, Parts: [][]byte{[]byte("message")}}
		good := envelope(t, Message, memory.MessageRecord{MessageID: "message", SourceID: "source"})
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, good) })
		rawExec(t, c, "UPDATE "+c.table("message")+" SET source_id=$1", []byte("foreign"))

		storedCode := memory.CodeStoreFailure

		for _, scan := range []bool{false, true} {
			err := c.View(t.Context(), func(tx *Tx) error {
				if scan {
					_, err := tx.Scan(t.Context(), Range{Family: Message, Scope: &scope, Limit: 1})

					return err
				}

				_, err := tx.Get(t.Context(), key)

				return err
			})
			assertCode(t, err, storedCode)
		}

		rawExec(t, c, "UPDATE "+c.table("message")+" SET envelope=$1", []byte("corrupt"))
		err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)

			return err
		})
		assertCode(t, err, storedCode)

		wrongID := envelope(t, Message, memory.MessageRecord{MessageID: "foreign", SourceID: "source"})
		for _, bad := range [][]byte{[]byte("corrupt"), envelope(t, Source, true), wrongID} {
			err := c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, bad) })
			assertCode(t, err, memory.CodeInvalidDerived)
		}

		oversized := bytes.Repeat([]byte{1}, MaxEnvelopeBytes+1)
		err = c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, oversized) })
		assertCode(t, err, memory.CodeLimitExceeded)
	})
}
