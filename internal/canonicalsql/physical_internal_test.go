package canonicalsql

import (
	"bytes"
	"strings"
	"testing"

	"slices"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/jackc/pgx/v5"
)

func TestNativeHashCollisionRetainsFullIdentity(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		// Fixed original frames collide in pinned PostgreSQL18's bytea hash opclass.
		keys := []Key{
			{Family: Payload, Parts: [][]byte{[]byte("10187")}},
			{Family: Payload, Parts: [][]byte{[]byte("159793")}},
		}

		assertNativeCollision(t, db, keys)

		for i, key := range keys {
			mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Payload, i)) })
		}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), keys[0], envelope(t, Payload, "updated")) })
		mustUpdate(t, c, func(tx *Tx) error { return tx.Delete(t.Context(), keys[0]) })

		if err := c.View(t.Context(), func(tx *Tx) error {
			_, err := tx.Get(t.Context(), keys[0])
			assertCode(t, err, memory.CodeNotFound)

			got, err := tx.Get(t.Context(), keys[1])
			if !bytes.Equal(got, envelope(t, Payload, 1)) {
				t.Fatal("collision aliased row")
			}

			if err != nil {
				return err
			}

			return tx.CheckIntegrity(t.Context())
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPostgresPhysicalDDL(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		if db.Postgres == nil {
			return
		}

		native, err := db.Postgres.Begin(t.Context())
		if err != nil {
			t.Fatal(err)
		}

		defer func() {
			if err := native.Rollback(t.Context()); err != nil {
				t.Error(err)
			}
		}()

		for _, statement := range strings.Split(postgresDDL, ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}

			if _, err := native.Exec(t.Context(), statement); err != nil {
				t.Fatalf("static DDL %q: %v", statement, err)
			}
		}

		assertPhysicalCatalog(t, native)

		assertPhysicalSequences(t, native)
	})
}

func assertNativeCollision(t *testing.T, db *canonicalsqltest.Database, keys []Key) {
	t.Helper()

	if db.Postgres != nil {
		var equal bool
		if err := db.Postgres.QueryRow(t.Context(), "SELECT hashbytea($1)=hashbytea($2)",
			stampKey(keys[0].Parts), stampKey(keys[1].Parts)).Scan(&equal); err != nil {
			t.Fatal(err)
		}

		if !equal {
			t.Fatal("fixture no longer collides under native bytea hash")
		}
	}
}

func assertPhysicalCatalog(t *testing.T, native pgx.Tx) {
	t.Helper()

	r, err := native.Query(t.Context(), `SELECT tab.relname,ix.relname,am.amname,i.indisunique,i.indexprs IS NOT NULL,
		 ARRAY(SELECT coalesce(a.attname,'<expression>') FROM unnest(i.indkey) WITH ORDINALITY k(num,ord)
		 LEFT JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.num ORDER BY k.ord)
		 FROM pg_index i JOIN pg_class ix ON ix.oid=i.indexrelid JOIN pg_class tab ON tab.oid=i.indrelid
		 JOIN pg_namespace n ON n.oid=tab.relnamespace JOIN pg_am am ON am.oid=ix.relam
		 WHERE n.nspname='boyan_canonical'`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	seen := map[string]map[string]bool{}

	for r.Next() {
		var (
			table, name, method string
			unique, expression  bool
			columns             []string
		)
		if err := r.Scan(&table, &name, &method, &unique, &expression, &columns); err != nil {
			t.Fatal(err)
		}

		assertIndexMembership(t, name, method, unique, expression, columns)

		if method == "hash" {
			if seen[table] == nil {
				seen[table] = map[string]bool{}
			}

			seen[table][columns[0]] = true
		}
	}

	if err := r.Err(); err != nil {
		t.Fatal(err)
	}

	r.Close()

	assertPartitionIndexes(t, seen)
}

func assertPhysicalSequences(t *testing.T, native pgx.Tx) {
	t.Helper()

	var sequences, cycling int
	// Identity sequences are exposed by pg_sequence, not information_schema.sequences.
	if err := native.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER (WHERE s.seqcycle) FROM pg_sequence s
 JOIN pg_class c ON c.oid=s.seqrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='boyan_canonical'`).Scan(&sequences, &cycling); err != nil {
		t.Fatal(err)
	}

	if sequences != 38 || cycling != 0 {
		t.Fatalf("private sequence contract: %d, cycling=%d", sequences, cycling)
	}
}

func assertIndexMembership(t *testing.T, name, method string, unique, expression bool, columns []string) {
	t.Helper()

	if expression {
		t.Errorf("expression index %s", name)
	}

	if method == "btree" {
		for _, col := range columns {
			if !slices.Contains([]string{"row_id", "stamp_id", "last_seq", "family", "singleton"}, col) {
				t.Errorf("unbounded B-tree membership %s.%s", name, col)
			}
		}
	}

	if method == "hash" {
		if unique || len(columns) != 1 {
			t.Errorf("invalid hash %s", name)
		}
	}
}

func assertPartitionIndexes(t *testing.T, seen map[string]map[string]bool) {
	t.Helper()

	for _, shape := range expectedShapes {
		if !seen[shape.table]["logical_key"] {
			t.Errorf("missing full-key hash %s", shape.table)
		}

		if shape.scoped && !seen[shape.table]["scope_partition"] {
			t.Errorf("missing scope hash %s", shape.table)
		}

		if len(shape.parts) == 2 && !seen[shape.table]["prefix_partition"] {
			t.Errorf("missing prefix hash %s", shape.table)
		}
	}

	if !seen["message"]["source_partition"] || !seen["revision"]["item_partition"] {
		t.Fatal("missing stamp/secondary hash")
	}
}
