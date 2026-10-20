package canonicalsql

import (
	"bytes"
	"context"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestMaximumCompoundKeysMutationAndRawPages(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := memory.Scope{Key: "chat:" + deterministicLongID(math.MaxUint16-5), Kind: memory.ScopeKindPersonal}
		long := []byte(deterministicLongID(math.MaxUint16))
		ids := [][]byte{[]byte("b"), []byte("aa"), []byte("a\x00:"), []byte("a"), long}

		for _, f := range []Family{Source, ProjectionManifest, EraseSourceRef, Payload} {
			t.Run(string(f), func(t *testing.T) {
				testMaxFamilyPage(t, c, scope, long, ids, f)
			})
		}

		if err := c.View(t.Context(), func(tx *Tx) error { return tx.CheckIntegrity(t.Context()) }); err != nil {
			t.Fatal(err)
		}
	})
}

type hydrationQuery struct {
	base       query
	hydrated   int
	statements []string
}

func (q *hydrationQuery) exec(ctx context.Context, s string, args ...any) (int64, error) {
	return q.base.exec(ctx, s, args...)
}
func (q *hydrationQuery) query(ctx context.Context, s string, args ...any) (rows, error) {
	q.statements = append(q.statements, s)

	r, err := q.base.query(ctx, s, args...)
	if err != nil {
		return nil, err
	}

	if strings.Contains(s, "envelope") {
		return &hydrationRows{rows: r, owner: q}, nil
	}

	return r, nil
}

type hydrationRows struct {
	rows

	owner *hydrationQuery
}

func (r *hydrationRows) Next() bool {
	next := r.rows.Next()
	if next {
		r.owner.hydrated++
	}

	return next
}

func TestRawKeyPageBoundsEnvelopeHydration(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()

		mustUpdate(t, c, func(tx *Tx) error {
			for _, id := range []string{"aa", "b", "z"} {
				key := Key{Family: Source, Scope: scope, Parts: [][]byte{[]byte(id)}}
				if err := tx.Put(t.Context(), key, envelope(t, Source, strings.Repeat("x", 16384))); err != nil {
					return err
				}
			}

			return nil
		})
		// The unselected envelope must never be decoded by a one-row page.
		rawExec(t, c, "UPDATE "+c.table("source")+" SET envelope=$1 WHERE id=$2", []byte("corrupt"), []byte("z"))

		if err := c.View(t.Context(), func(tx *Tx) error {
			recorder := &hydrationQuery{base: tx.q}
			tx.q = recorder

			page, err := tx.Scan(t.Context(), Range{Family: Source, Scope: &scope, Limit: 1})
			if err != nil {
				return err
			}

			if len(page) != 1 || string(page[0].Key.Parts[0]) != "aa" || recorder.hydrated != 1 {
				t.Fatal("hydration/page bound changed")
			}

			if db.Postgres != nil {
				assertKeyPageBarrier(t, recorder.statements[0])
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRevisionSecondaryUnsignedCursor(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()
		item := deterministicLongID(65535)

		mustUpdate(t, c, func(tx *Tx) error {
			for _, value := range []memory.MemoryRevision{
				{RevisionID: "z", ItemID: item, Revision: math.MaxInt64 + 1},
				{RevisionID: "aa", ItemID: item, Revision: math.MaxInt64 + 1},
				{RevisionID: "a", ItemID: item, Revision: math.MaxUint64},
			} {
				key := Key{Family: Revision, Scope: scope, Parts: [][]byte{[]byte(value.RevisionID)}}
				if err := tx.Put(t.Context(), key, envelope(t, Revision, value)); err != nil {
					return err
				}
			}

			return nil
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			var (
				after *RevisionCursor
				ids   []string
			)

			for {
				page, err := tx.RevisionsByItem(t.Context(), scope, []byte(item), after, 1)
				if err != nil {
					return err
				}

				if len(page) == 0 {
					break
				}

				var value memory.MemoryRevision
				if err := Decode(Revision, page[0].Envelope, &value); err != nil {
					return err
				}

				ids = append(ids, value.RevisionID)
				after = &RevisionCursor{Revision: value.Revision, ID: []byte(value.RevisionID)}
			}

			if !slices.Equal(ids, []string{"aa", "z", "a"}) {
				t.Fatal("unsigned secondary cursor lost/reordered rows")
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func testMaxFamilyPage(t *testing.T, c *Connection, scope memory.Scope, long []byte, ids [][]byte, f Family) {
	t.Helper()

	keys := make([]Key, len(ids))
	for i, id := range ids {
		keys[i] = Key{Family: f, Scope: scope, Parts: [][]byte{id}}
		if f == Payload {
			keys[i].Scope = memory.Scope{}
		}

		if f == ProjectionManifest || f == EraseSourceRef {
			keys[i].Parts = [][]byte{long, id}
		}
	}

	mustUpdate(t, c, func(tx *Tx) error {
		for _, key := range keys {
			if err := tx.Put(t.Context(), key, envelope(t, f, true)); err != nil {
				return err
			}
		}

		return nil
	})

	request := Range{Family: f, Scope: &scope, Limit: 1}
	if f == Payload {
		request.Scope = nil
	}

	if f == ProjectionManifest || f == EraseSourceRef {
		request.Prefix = [][]byte{long}
	}

	assertRawPages(t, c, request, ids)

	key := keys[len(keys)-1]

	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, f, false)) })
	mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), key) })
	mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), key) })
	mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, f, "reinserted")) })
}

func assertRawPages(t *testing.T, c *Connection, request Range, ids [][]byte) {
	t.Helper()

	if err := c.View(t.Context(), func(tx *Tx) error {
		var got [][]byte

		for {
			page, err := tx.Scan(t.Context(), request)
			if err != nil {
				return err
			}

			if len(page) == 0 {
				break
			}

			if len(page) != 1 {
				t.Fatal("page bound")
			}

			key := page[0].Key
			got = append(got, key.Parts[len(key.Parts)-1])
			request.After = &key
		}

		want := slices.Clone(ids)
		slices.SortFunc(want, bytes.Compare)

		if !slices.EqualFunc(got, want, bytes.Equal) {
			t.Fatal("raw byte order/continuation changed")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertKeyPageBarrier(t *testing.T, statement string) {
	t.Helper()

	cte, _, ok := strings.Cut(statement, ") SELECT ")
	if !ok || !strings.Contains(cte, "AS MATERIALIZED") || strings.Contains(cte, "envelope") ||
		!strings.Contains(cte, "LIMIT") ||
		!strings.Contains(statement, "r.row_id=p.row_id ORDER BY r.scope_key,r.scope_kind,r.id") {
		t.Fatal("raw key-page barrier/order missing")
	}
}
