package canonicalsql

import (
	"context"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsqltest"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Losing the session lock must permanently fence this owner even when the
// underlying socket still works. Acquiring it again cannot revive the owner.
func TestPostgresOwnerLostLockStaysPoisoned(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	owner := openPostgresTestOwner(t, PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true})
	c := owner.connection
	rawExec(t, c, "SELECT pg_catalog.pg_advisory_unlock_all()")
	assertCode(t, owner.RestorePreflight(t.Context()), memory.CodeUnavailable)
	rawExec(t, c, "SELECT pg_catalog.pg_advisory_lock(1651472750,1667329647)")
	err := owner.Update(t.Context(), func(context.Context, *Tx) error {
		t.Error("poisoned owner admitted callback")

		return nil
	})
	assertCode(t, err, memory.CodeUnavailable)
}

// Readiness must detect stored duplicate identities after open, not just rely
// on the nonunique HASH index or the earlier migration integrity check.
func TestPostgresOwnerReadinessRejectsDuplicate(t *testing.T) {
	db := canonicalsqltest.PostgresDatabase(t)
	config := PostgresConfig{DSN: db.DSN, AllowLocalInsecure: true}
	owner := openPostgresTestOwner(t, config)
	c := owner.connection
	state := memory.ScopeState{
		SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: testScope(), Version: 1, ChangeSeq: 1,
	}

	if err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		return tx.CompareAndSwapScope(ctx, nil, state)
	}); err != nil {
		t.Fatal(err)
	}

	corruptScopeFixture(t, c, "duplicate_row")
	assertCode(t, owner.RestorePreflight(t.Context()), memory.CodeStoreFailure)
	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		_ = tx.Put(ctx, Key{Family: ScopeState, Scope: state.Scope}, envelope(t, ScopeState, state))

		return nil // Swallowing duplicate rejection must not acknowledge commit.
	})
	assertCode(t, err, memory.CodeStoreFailure)
	assertClock(t, c, 1, 1)

	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}

	failed, err := OpenPostgresOwner(t.Context(), config)
	if failed != nil {
		_ = failed.Close()

		t.Fatal("duplicate identities survived migration integrity")
	}

	assertCode(t, err, memory.CodeStoreFailure)
}
