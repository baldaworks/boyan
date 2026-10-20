//go:build integration

package canonicalsql

import (
	"strconv"
	"strings"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestLegacyNamespaceUsesTargetedExistencePath(t *testing.T) {
	canonicalsqltest.ForEach(t, func(t *testing.T, db *canonicalsqltest.Database) {
		c := testConnection(t, db)
		scope := testScope()

		mustUpdate(t, c, func(tx *Tx) error {
			for i := range 1024 {
				key := Key{Family: Operation, Scope: scope, Parts: [][]byte{[]byte("native:" + strconv.Itoa(i))}}
				if err := tx.Put(t.Context(), key, envelope(t, Operation, true)); err != nil {
					return err
				}
			}

			return nil
		})

		assertLegacyNamespace(t, c, scope, false)

		id := []byte("boyan-derived:v1:operation:" + strings.Repeat("!", 64))
		key := Key{Family: Operation, Scope: scope, Parts: [][]byte{id}}

		mustUpdate(t, c, func(tx *Tx) error { return tx.Put(t.Context(), key, envelope(t, Operation, true)) })
		// Namespace occupancy is not record decoding: even an opaque legacy value
		// and a nonhex suffix must fence it. Integrity would reject this bad value.
		rawExec(t, c, "UPDATE "+c.table("operation")+" SET envelope=$1 WHERE id=$2", []byte("opaque legacy"), key.Parts[0])
		assertLegacyNamespace(t, c, scope, true)
	})
}

func assertLegacyNamespace(t *testing.T, c *Connection, scope memory.Scope, want bool) {
	t.Helper()

	if err := c.View(t.Context(), func(tx *Tx) error {
		recorder := &hydrationQuery{base: tx.q}
		tx.q = recorder

		found, err := tx.HasLegacyOperationNamespace(t.Context(), scope)
		if err != nil {
			return err
		}

		if found != want || recorder.hydrated != 0 {
			t.Fatal("namespace existence/hydration changed")
		}

		statement := recorder.statements[0]
		if strings.Contains(statement, "ORDER BY") || strings.Contains(statement, "envelope") {
			t.Fatal("legacy guard scanned ordered history/envelopes")
		}

		if c.pg != nil {
			return assertLegacyIndexPlan(t, tx, scope, statement)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyIndexPlan(t *testing.T, tx *Tx, scope memory.Scope, statement string) error {
	t.Helper()

	if !strings.Contains(statement, "scope_partition=$3 AND length(id)=91") {
		t.Fatal("legacy partial predicate changed")
	}

	if _, err := tx.q.exec(t.Context(), "SET LOCAL enable_seqscan=off"); err != nil {
		return err
	}

	r, err := tx.q.query(t.Context(), "EXPLAIN "+statement,
		[]byte(scope.Key), []byte(scope.Kind), stampKey([][]byte{[]byte(scope.Key), []byte(scope.Kind)}))
	if err != nil {
		return err
	}
	defer r.Close()

	var plan strings.Builder

	for r.Next() {
		var line string
		if err := r.Scan(&line); err != nil {
			return err
		}

		plan.WriteString(line)
	}

	if err := r.Err(); err != nil {
		return err
	}

	if !strings.Contains(plan.String(), "operation_legacy_namespace_idx") || strings.Contains(plan.String(), "Sort") {
		t.Fatalf("targeted partial index unavailable: %s", plan.String())
	}

	return nil
}
