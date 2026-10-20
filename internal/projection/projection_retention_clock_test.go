//go:build integration

package projection //nolint:testpackage // Exercises the real coordinator and canonical/search publication boundary.

import (
	"context"
	"fmt"

	"testing"
	"time"

	"github.com/baldaworks/boyan/pkg/memory"
)

func TestProjectionRetentionUsesPublicationOrder(t *testing.T) {
	runProjectionAuthorities(t, func(t *testing.T, newCanonical projectionOwnerFactory) {
		for _, test := range []struct {
			name    string
			offsets []int
		}{
			{name: "backward", offsets: []int{1, 10, 3, 4, 5, 6}},
			{name: "equal", offsets: []int{1, 1, 1, 1, 1, 1}},
		} {
			t.Run(test.name, func(t *testing.T) {
				store, applier, runtime := retentionClockRuntime(t, test.offsets, newCanonical)
				scope := memory.Scope{Kind: memory.ScopeKindPersonal, Key: "test:retention-clock"}
				other := memory.Scope{Kind: memory.ScopeKindGroup, Key: scope.Key}
				requireNoError(t, runtime.Sync(t.Context(), other), "%v")

				stopSearch, searchErrors := startRetentionSearch(applier.store, other)
				defer stopSearch()

				for i := range test.offsets {
					// Fixed generation names make the equal-time counterexample
					// deterministic: key order cannot substitute for publication order.
					_, err := runtime.coordinator.Rebuild(t.Context(), scope, "sqlite", fmt.Sprintf("generation-%d", i))
					requireNoError(t, err, "%v")
				}

				var builds int
				requireNoError(
					t,
					applier.db.QueryRowContext(t.Context(), "SELECT count(*) FROM lexical_build").Scan(&builds),
					"%v",
				)

				if builds > 3 {
					t.Fatalf("retained %d builds, want at most active+previous plus other scope", builds)
				}

				assertClockPublication(t, applier, scope)

				if _, found, err := store.LoadActiveProjectionManifest(t.Context(), other, "sqlite"); err != nil || !found {
					t.Fatalf("other scope lost: %v %v", found, err)
				}

				select {
				case err := <-searchErrors:
					t.Fatalf("search during retention: %v", err)
				default:
				}
			})
		}
	})
}

func retentionClockRuntime(
	t *testing.T,
	offsets []int,
	newCanonical projectionOwnerFactory,
) (projectionTestOwner, *projectionSQLApplier, *ProjectionRuntime) {
	t.Helper()
	root := t.TempDir()
	store, reader := func() (projectionTestOwner, memory.RecallCanonicalReader) {
		return newCanonical(t)
	}()

	t.Cleanup(func() { requireNoError(t, store.Close(), "%v") })
	applier := openProjectionSQLite(t, root, store, reader)
	t.Cleanup(func() { requireNoError(t, applier.Close(), "%v") })

	checkpoints := &retentionClockStore{projectionTestOwner: store, offsets: offsets}
	runtime, err := NewProjectionRuntime(store, checkpoints, applier, "sqlite", 1)
	requireNoError(t, err, "%v")
	t.Cleanup(func() { requireNoError(t, runtime.Close(context.Background()), "%v") })

	return store, applier, runtime
}

type retentionClockStore struct {
	projectionTestOwner

	offsets     []int
	activations int
}

func (store *retentionClockStore) ActivateProjectionGeneration(ctx context.Context, manifest memory.ProjectionManifest,
	at time.Time) error {
	if manifest.Scope.Kind == memory.ScopeKindPersonal {
		at = time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC).
			Add(time.Duration(store.offsets[store.activations]) * time.Second)
		store.activations++
	}

	return store.projectionTestOwner.ActivateProjectionGeneration(ctx, manifest, at)
}

func assertClockPublication(t *testing.T, applier *projectionSQLApplier, scope memory.Scope) {
	t.Helper()

	for _, pointer := range []struct{ table, want string }{
		{table: "lexical_active", want: "generation-5"},
		{table: "lexical_previous", want: "generation-4"},
	} {
		var generation string

		query := "SELECT b.generation_id FROM " + pointer.table +
			" p JOIN lexical_build b USING(physical_build_id,scope_kind,scope_key) WHERE p.scope_kind=? AND p.scope_key=?"
		requireNoError(t, applier.db.QueryRowContext(t.Context(), query, scope.Kind, scope.Key).Scan(&generation), "%v")

		if generation != pointer.want {
			t.Fatalf("%s points to %q, want %q", pointer.table, generation, pointer.want)
		}
	}
}
