//go:build integration

package canonicalsql

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func rawExec(t *testing.T, c *Connection, statement string, args ...any) {
	t.Helper()

	if _, err := c.q.exec(t.Context(), statement, args...); err != nil {
		t.Fatal(databaseError(t.Context(), "test fixture", err))
	}
}

func assertCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()

	code, _, ok := memory.ClassifyError(err)
	if !ok || code != want {
		t.Fatalf("error classification=%s, want %s (%v)", code, want, err)
	}
}

// Catches committing rows/clock when a later stamp fails, including a swallowed
// write error. Real database constraints inject the failure after a row write.
func TestMultirowWriteFailureRollsBackEverything(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		if db.SQLite != nil {
			rawExec(t, c, "CREATE TRIGGER reject_item_stamp BEFORE INSERT ON item "+
				"BEGIN SELECT RAISE(ABORT,'secret-payload'); END")
		} else {
			rawExec(t, c, "ALTER TABLE boyan_canonical.item ADD CONSTRAINT reject_item_stamp "+
				"CHECK(id<>decode('6f6e65','hex'))")
		}

		key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("one")}}

		err := c.Update(t.Context(), func(tx *Tx) error {
			if err := tx.Put(t.Context(), key, envelope(t, Source, true)); err != nil {
				return err
			}

			_ = tx.Put(t.Context(), Key{Family: Item, Scope: key.Scope, Parts: key.Parts}, envelope(t, Item, "secret-payload"))

			return nil
		})
		if err == nil {
			t.Fatal("failed stamp committed")
		}

		if strings.Contains(err.Error(), "secret-payload") {
			t.Fatal("SQL payload escaped error")
		}

		if _, _, ok := memory.ClassifyError(err); !ok {
			t.Fatal("unclassified database error")
		}

		var count int
		if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("source"), nil, &count); err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatal("partial row survived")
		}

		if err := scanOne(t.Context(), c.q, "SELECT count(*) FROM "+c.table("item"), nil, &count); err != nil {
			t.Fatal(err)
		}

		if count != 0 {
			t.Fatal("row without stamp survived")
		}
	})
}

// Deletion and reinsertion must preserve absence and exact replacement bytes.
func TestTransactionDeleteAndReinsert(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("same-key")}}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, "first")) })
		mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), key) })

		if err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), key)
			assertCode(t, err, memory.CodeNotFound)

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		want := envelope(t, Source, "replacement")
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, want) })

		if err := c.View(t.Context(), func(tx *Tx) error {
			got, err := tx.Get(t.Context(), key)
			if !bytes.Equal(got, want) {
				t.Fatal("reinsertion returned stale bytes")
			}

			return err
		}); err != nil {
			t.Fatal(err)
		}
	})
}

// Catches one clock per row rather than transaction, retained old plaintext,
// missing tombstones, and reinsertion failing to replace a deletion stamp.
// Catches signed CAS, trusting mismatched columns, and committing a second
// same-version mutation. Scope increments themselves belong to domain callers.
func TestScopeCASAndEnvelopeColumnValidation(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		state := memory.ScopeState{
			SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: testScope(),
			Version: math.MaxInt64 + 1, ChangeSeq: math.MaxUint64,
		}

		mustUpdate(t, c, func(tx *Tx) error { return tx.CompareAndSwapScope(t.Context(), nil, state) })

		expected := state.Version
		state.Version++

		mustUpdate(t, c, func(tx *Tx) error { return tx.CompareAndSwapScope(t.Context(), &expected, state) })
		err := c.Update(t.Context(), func(tx *Tx) error { return tx.CompareAndSwapScope(t.Context(), &expected, state) })
		assertCode(t, err, memory.CodeConflict)

		rawExec(t, c, "UPDATE "+c.table("scope_state")+" SET version_u64=$1", Uint64(7))

		storedCode := memory.CodeStoreFailure

		err = c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), Key{Family: ScopeState, Scope: state.Scope})

			return err
		})
		assertCode(t, err, storedCode)

		expected = 7
		err = c.Update(t.Context(), func(tx *Tx) error { return tx.CompareAndSwapScope(t.Context(), &expected, state) })
		assertCode(t, err, storedCode)

		if _, err := c.q.exec(t.Context(), "UPDATE "+c.table("scope_state")+" SET change_seq_u64=$1", []byte{1}); err == nil {
			t.Fatal("malformed uint column accepted")
		}
	})
}

// Catches wraparound and the row being committed before clock exhaustion fails.
// Catches canceled SQLite transactions retaining a write lock or publishing
// effects; cancellation must take precedence over a callback's other failure.
func TestContextRollbackAndHandleLifetime(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		key := Key{Family: Source, Scope: testScope(), Parts: [][]byte{[]byte("id")}}

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		var retained *Tx

		err := c.Update(ctx, func(tx *Tx) error {
			retained = tx
			if err := tx.Put(ctx, key, envelope(t, Source, true)); err != nil {
				return err
			}

			cancel()

			return errors.New("callback failed")
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}

		if err := retained.Delete(t.Context(), key); err == nil {
			t.Fatal("escaped transaction remains usable")
		}

		err = c.View(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })
		assertCode(t, err, memory.CodeInvalidDerived)
		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Source, true)) })
	})
}
