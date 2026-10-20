package canonicalsql

import (
	"bytes"
	"math"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func mustUpdate(t *testing.T, c *Connection, fn func(*Tx) error) {
	t.Helper()

	if err := c.Update(t.Context(), fn); err != nil {
		t.Fatal(err)
	}
}

// Catches length-prefixed sorting, signed uint64 comparison, lost exact scope,
// inclusive cursors, and returning more than the requested page.
func TestBinaryRangesAndUnsignedDomain(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		group := scope
		group.Kind = memory.ScopeKindGroup
		other := scope
		other.Key = "chat:other"
		ids := [][]byte{[]byte("a"), []byte("a\x00"), []byte("a:b|c"), []byte("aa"), []byte("b"), {255}}

		mustUpdate(t, c, func(tx *Tx) error {
			for _, s := range []memory.Scope{scope, group, other} {
				for _, id := range ids {
					key := Key{Family: Source, Scope: s, Parts: [][]byte{id}}
					if err := tx.Put(t.Context(), key, envelope(t, Source, s.Key+string(s.Kind))); err != nil {
						return err
					}
				}
			}

			for _, value := range []uint64{0, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64} {
				key := Key{Family: ScopeChange, Scope: scope, Parts: [][]byte{Uint64(value)}}
				if err := tx.Put(t.Context(), key, envelope(t, ScopeChange, true)); err != nil {
					return err
				}
			}

			return nil
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			assertBinaryPages(t, tx, scope, ids)
			assertUnsignedPage(t, tx, scope)

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		for _, limit := range []int{0, -1, memory.MaxCanonicalReadRecords + 1} {
			err := c.View(t.Context(), func(tx *Tx) error {
				_, err := tx.Scan(t.Context(), Range{Family: Source, Scope: &scope, Limit: limit})

				return err
			})
			if err == nil {
				t.Fatal("invalid bound accepted")
			}
		}
	})
}

// Catches tuple collisions and applying a cursor/prefix to the wrong partition.
func TestCompoundPrefixAndGlobalIsolation(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		keys := []Key{
			{Family: ProvenanceForward, Scope: scope, Parts: [][]byte{[]byte("a\x00b"), []byte("c")}},
			{Family: ProvenanceForward, Scope: scope, Parts: [][]byte{[]byte("a"), []byte("b\x00c")}},
			{Family: ProvenanceForward, Scope: scope, Parts: [][]byte{[]byte("a"), []byte("long")}},
		}

		mustUpdate(t, c, func(tx *Tx) error {
			for i, key := range keys {
				if err := tx.Put(t.Context(), key, envelope(t, key.Family, i)); err != nil {
					return err
				}
			}

			return tx.Put(t.Context(), Key{Family: Payload, Parts: [][]byte{[]byte("a")}}, envelope(t, Payload, "global"))
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			for i, key := range keys {
				got, err := tx.Get(t.Context(), key)
				if err != nil {
					return err
				}

				var value int
				if err := Decode(key.Family, got, &value); err != nil {
					return err
				}

				if value != i {
					t.Fatal("tuple identity collision")
				}
			}

			request := Range{Family: ProvenanceForward, Scope: &scope, Prefix: [][]byte{[]byte("a")}, Limit: 1}

			page, err := tx.Scan(t.Context(), request)
			if err != nil {
				return err
			}

			assertPrefixPage(t, page)

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRangeRejectsForeignCursor(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		foreign := scope
		foreign.Kind = memory.ScopeKindGroup

		cursors := []Key{
			{Family: Source, Scope: scope, Parts: [][]byte{[]byte("id")}},
			{Family: ProvenanceForward, Scope: foreign, Parts: [][]byte{[]byte("from"), []byte("to")}},
			{Family: ProvenanceForward, Scope: scope, Parts: [][]byte{[]byte("wrong"), []byte("to")}},
		}
		for _, cursor := range cursors {
			err := c.View(t.Context(), func(tx *Tx) error {
				request := Range{
					Family: ProvenanceForward, Scope: &scope, Prefix: [][]byte{[]byte("from")}, After: &cursor, Limit: 1,
				}
				_, err := tx.Scan(t.Context(), request)

				return err
			})
			assertCode(t, err, memory.CodeInvalidDerived)
		}
	})
}

func assertPrefixPage(t *testing.T, page []Row) {
	t.Helper()

	if len(page) != 1 || string(page[0].Key.Parts[1]) != "b\x00c" {
		t.Fatal("prefix boundary/order lost")
	}
}

func TestGlobalKeysRejectUserScopes(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()

		for _, f := range []Family{Payload, SourcePrivacyReady, EraseGuard, EraseBlobLock, ErasedBlob} {
			key := Key{Family: f, Scope: scope}
			if f == Payload || f == EraseBlobLock || f == ErasedBlob {
				key.Parts = [][]byte{[]byte("a")}
			}
			// Valid shape/family envelope prevents an arity or envelope error from
			// accidentally standing in for the scope prohibition under test.
			encoded := envelope(t, f, true)

			err := c.Update(t.Context(), func(tx *Tx) error { return tx.Put(t.Context(), key, encoded) })
			if err == nil {
				t.Fatalf("global/control family accepted user scope: %s", f)
			}
		}
	})
}

func assertBinaryPages(t *testing.T, tx *Tx, scope memory.Scope, ids [][]byte) {
	t.Helper()

	var got [][]byte

	request := Range{Family: Source, Scope: &scope, Limit: 2}
	for {
		page, err := tx.Scan(t.Context(), request)
		if err != nil {
			t.Fatal(err)
		}

		if len(page) > 2 {
			t.Fatal("unbounded page")
		}

		if len(page) == 0 {
			break
		}

		for _, row := range page {
			if row.Key.Scope != scope {
				t.Fatal("scope leaked")
			}

			got = append(got, row.Key.Parts[0])
		}

		request.After = &page[len(page)-1].Key
	}

	if len(got) != len(ids) {
		t.Fatalf("got %d identities, want %d", len(got), len(ids))
	}

	for i := range ids {
		if !bytes.Equal(got[i], ids[i]) {
			t.Fatalf("byte ordering at %d: %x != %x", i, got[i], ids[i])
		}
	}
}

func assertUnsignedPage(t *testing.T, tx *Tx, scope memory.Scope) {
	t.Helper()

	changes, err := tx.Scan(t.Context(), Range{Family: ScopeChange, Scope: &scope, Limit: 4})
	if err != nil {
		t.Fatal(err)
	}

	if len(changes) != 4 {
		t.Fatal("missing unsigned sequences")
	}

	for i, want := range []uint64{0, math.MaxInt64, math.MaxInt64 + 1, math.MaxUint64} {
		got, err := DecodeUint64(changes[i].Key.Parts[0])
		if err != nil || got != want {
			t.Fatalf("unsigned sequence %d: %d, %v", i, got, err)
		}
	}
}
