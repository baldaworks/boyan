package badger //nolint:testpackage // Exercises durable pagination and key/record identity checks.

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestCanonicalScopeStatesPageSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store, err := OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	scopes := []memory.Scope{
		{Key: "startup:same", Kind: memory.ScopeKindPersonal},
		{Key: "startup:same", Kind: memory.ScopeKindGroup},
		{Key: "startup:other", Kind: memory.ScopeKindPersonal},
	}
	for _, scope := range scopes {
		key, err := badgerScopeKey(scope)
		requireEraseTestNoError(t, err)
		requireEraseTestNoError(t, store.db.Update(func(txn *badger.Txn) error {
			return putCanonicalRecord(txn, key, badgerRecordScope,
				memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope})
		}))
	}

	first, cursor, err := store.ListCanonicalScopeStates(ctx, nil, 1)
	requireEraseTestNoError(t, err)

	if len(first) != 1 || len(cursor) == 0 {
		t.Fatalf("first=%v cursor=%x", first, cursor)
	}

	requireEraseTestNoError(t, store.Close())
	store, err = OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	seen := map[memory.Scope]bool{first[0].Scope: true}

	for len(cursor) > 0 {
		page, next, err := store.ListCanonicalScopeStates(ctx, cursor, 1)
		requireEraseTestNoError(t, err)

		for _, state := range page {
			if seen[state.Scope] {
				t.Fatal("exclusive cursor repeated scope")
			}

			seen[state.Scope] = true
		}

		cursor = next
	}

	if len(seen) != len(scopes) {
		t.Fatalf("scopes=%v", seen)
	}

	assertScopePageBounds(t, store)
}

func TestDirtyActiveManifestRemainsReadableForStartupRepair(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	manifest := memory.ProjectionManifest{
		Scope:        memory.Scope{Key: "startup:dirty", Kind: memory.ScopeKindPersonal},
		ProjectionID: "test", GenerationID: "generation", Status: memory.ProjectionGenerationBuilding,
		UpdatedAt: time.Now().UTC(),
	}
	requireEraseTestNoError(t, store.MarkProjectionDirty(ctx, manifest))
	requireEraseTestNoError(t, store.ActivateProjectionGeneration(ctx, manifest, time.Now().UTC()))
	manifest.Status = memory.ProjectionGenerationActive
	requireEraseTestNoError(t, store.MarkProjectionDirty(ctx, manifest))
	dirty, found, err := store.LoadActiveProjectionManifest(ctx, manifest.Scope, manifest.ProjectionID)
	requireEraseTestNoError(t, err)

	if !found || dirty.Status != memory.ProjectionGenerationDirty {
		t.Fatalf("manifest=%+v found=%v, want validated dirty checkpoint", dirty, found)
	}
}

func assertScopePageBounds(t *testing.T, store *CanonicalStore) {
	t.Helper()

	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, _, err := store.ListCanonicalScopeStates(canceled, nil, 1); err == nil {
		t.Fatal("pagination ignored cancellation")
	}

	if _, _, err := store.ListCanonicalScopeStates(ctx, nil, memory.MaxCanonicalReadRecords+1); err == nil {
		t.Fatal("pagination accepted unbounded page")
	}
}
