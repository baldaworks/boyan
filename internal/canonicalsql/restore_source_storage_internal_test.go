package canonicalsql

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRestoreSourceLongScopesBoundedCopy(t *testing.T) {
	forEachOwner(t, func(t *testing.T, source *Owner, _ func() *Owner) {
		longScope := memory.Scope{Key: "test:" + strings.Repeat("k", 65530), Kind: memory.ScopeKindPersonal}
		sourceMarker(t, source, longScope)

		if err := source.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			for n := range 257 {
				scope := memory.Scope{Key: fmt.Sprintf("test:scope:%03d", n), Kind: longScope.Kind}
				if err := persistBoundaryCapability(ctx, tx, scope); err != nil {
					return err
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		full, base := sourceBackup(t, source, 0)

		destination, _ := restoreSourceDestination(t, source)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(full), 32); err != nil {
			t.Fatal(err)
		}

		sourceOperation(t, source, testScope(), "native-copy-trigger", 0)

		delta, _ := sourceBackup(t, source, base)
		if err := destination.RestoreCanonicalBackup(t.Context(), bytes.NewReader(delta), 32); err != nil {
			t.Fatal(err)
		}

		requireSourceCapability(t, destination, longScope, true)

		var count, size int
		if err := scanOne(t.Context(), destination.connection.q,
			"SELECT count(*) FROM "+destination.connection.table("restore_source_capability"),
			nil, &count); err != nil || count != 258 {
			t.Fatalf("bounded generation copy/cleanup count=%d: %v", count, err)
		}

		if err := scanOne(t.Context(), destination.connection.q,
			"SELECT length(control) FROM "+destination.connection.table("restore_chain"),
			nil, &size); err != nil || size > 4096 {
			t.Fatalf("scope set escaped bounded control: %d %v", size, err)
		}
	})
}

//nolint:cyclop,gocognit,funlen // Native HASH collision, deletion, duplicate rejection, and stamps share one fixture.
func TestRestoreSourcePrivateExactIdentityAndIndexes(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		generation := strings.Repeat("11", 16)

		scopes := []memory.Scope{
			{Key: "test:first", Kind: memory.ScopeKindPersonal},
			{Key: "test:second", Kind: memory.ScopeKindPersonal},
		}
		if c.pg != nil {
			// Fixed framed original identities collide in the pinned native HASH opclass.
			scopes[0].Key, scopes[1].Key = "test:3568", "test:157523"

			gen, err := hex.DecodeString(generation)
			if err != nil {
				t.Fatal(err)
			}

			var equal bool
			if err := scanOne(t.Context(), c.q, "SELECT hashbytea($1)=hashbytea($2)", []any{
				stampKey([][]byte{gen, []byte(scopes[0].Key), []byte(scopes[0].Kind)}),
				stampKey([][]byte{gen, []byte(scopes[1].Key), []byte(scopes[1].Kind)}),
			}, &equal); err != nil || !equal {
				t.Fatalf("source fixture no longer collides: %v", err)
			}

			assertSourceIndexes(t, c)
		}

		mustUpdate(t, c, func(tx *Tx) error {
			for _, scope := range scopes {
				if err := tx.putRestoreSource(t.Context(), generation, scope, false); err != nil {
					return err
				}
			}

			return tx.deleteRestoreSource(t.Context(), generation, scopes[0])
		})

		if err := c.View(t.Context(), func(tx *Tx) error {
			for i, scope := range scopes {
				found, err := tx.hasRestoreSource(t.Context(), generation, scope)
				if err != nil || found != (i == 1) {
					t.Fatalf("native collision aliased exact scope: %v %v", found, err)
				}
			}

			return nil
		}); err != nil {
			t.Fatal(err)
		}

		assertClock(t, c, 0, 0)

		if _, err := familyDefinition(Family("restore_source_capability")); err == nil {
			t.Fatal("private provenance became a canonical family")
		}

		if c.pg != nil {
			rawExec(t, c, "INSERT INTO "+c.table("restore_source_capability")+
				"(generation,logical_key,scope_key,scope_kind) SELECT generation,logical_key,scope_key,scope_kind FROM "+
				c.table("restore_source_capability"))

			if err := c.View(t.Context(), func(tx *Tx) error {
				_, err := tx.hasRestoreSource(t.Context(), generation, scopes[1])

				return err
			}); err == nil {
				t.Fatal("duplicate exact source evidence was accepted")
			}
		}
	})
}

//nolint:cyclop // Inspect every physical index member, including uniqueness and the allowed fixed-width B-tree keys.
func assertSourceIndexes(t *testing.T, c *Connection) {
	t.Helper()

	r, err := c.q.query(t.Context(), `SELECT am.amname,i.indisunique,a.attname FROM pg_index i
	 JOIN pg_class tab ON tab.oid=i.indrelid JOIN pg_namespace ns ON ns.oid=tab.relnamespace
	 JOIN pg_class ix ON ix.oid=i.indexrelid JOIN pg_am am ON am.oid=ix.relam
	 JOIN LATERAL unnest(i.indkey) k(num) ON true JOIN pg_attribute a ON a.attrelid=tab.oid AND a.attnum=k.num
	 WHERE ns.nspname='boyan_canonical' AND tab.relname='restore_source_capability'`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	hashFound := false

	for r.Next() {
		var method, column string

		var unique bool
		if err := r.Scan(&method, &unique, &column); err != nil {
			t.Fatal(err)
		}

		if method == "btree" && column != "row_id" && column != "generation" {
			t.Fatalf("variable length source identity indexed by B-tree: %s", column)
		}

		if method == "hash" {
			if unique || column != "logical_key" {
				t.Fatal("source identity HASH is not nonunique exact-key lookup")
			}

			hashFound = true
		}
	}

	if err := r.Err(); err != nil || !hashFound {
		t.Fatalf("source HASH index missing: %v", err)
	}
}
