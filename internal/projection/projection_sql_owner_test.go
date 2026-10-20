//go:build integration

package projection //nolint:testpackage // Reuses the actual disposable SQLite runtime fixture.

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

type projectionSQLOwner interface {
	memory.CanonicalStore
	memory.ProjectionCheckpointStore
	memory.CanonicalOperationCommitter
	memory.ProjectionRetentionFloorWriter
	projectionCanonicalAuthority
	PrepareSourcePrivacy(ctx context.Context) error
}

//nolint:cyclop,funlen // Real SQL recovery and rebuild assertions share one durable lifecycle.
func TestProjectionRuntimeSQLAuthorityRecoveryAndRebuild(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		canonical := fixture.Store.(projectionSQLOwner)
		requireNoError(t, canonical.PrepareSourcePrivacy(t.Context()), "%v")

		var reader memory.RecallCanonicalReader

		switch store := fixture.Store.(type) {
		case *sqlite.Store:
			reader = sqlite.NewCanonicalReader(store)
		case *postgres.Store:
			reader = postgres.NewCanonicalReader(store)
		}

		root := t.TempDir()
		open := func() (*ProjectionRuntime, *failedCommitApplier) {
			applier := &failedCommitApplier{projectionSQLApplier: openProjectionSQLite(t, root, canonical, reader)}
			runtime, err := NewProjectionRuntime(canonical, canonical, applier, "sqlite", 1)
			requireNoError(t, err, "%v")

			return runtime, applier
		}
		runtime, applier := open()
		scope := memory.Scope{Key: "test:sql-projection", Kind: memory.ScopeKindPersonal}
		_, err := canonical.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{
			Scope: scope, OperationID: "one", Fingerprint: "one", CommittedAt: time.Now().UTC()})
		requireNoError(t, err, "%v")
		requireNoError(t, runtime.Sync(t.Context(), scope), "%v")

		first, found, err := canonical.LoadActiveProjectionManifest(t.Context(), scope, "sqlite")
		if err != nil || !found || first.Watermark != 1 {
			t.Fatalf("first=%+v %t %v", first, found, err)
		}

		_, err = canonical.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{
			Scope:                scope,
			OperationID:          "two",
			Fingerprint:          "two",
			ExpectedScopeVersion: 1,
			CommittedAt:          time.Now().UTC(),
		})
		requireNoError(t, err, "%v")

		applier.fail = true

		if err := runtime.Sync(t.Context(), scope); err == nil {
			t.Fatal("physical commit interruption was ignored")
		}

		requireNoError(t, runtime.Close(t.Context()), "%v")
		requireNoError(t, applier.Close(), "%v")

		runtime, applier = open()
		defer applier.Close()
		defer runtime.Close(context.Background())

		requireNoError(t, runtime.Start(t.Context()), "%v")

		active, found, err := canonical.LoadActiveProjectionManifest(t.Context(), scope, "sqlite")
		if err != nil || !found || active.Status != memory.ProjectionGenerationActive || active.Watermark != 2 {
			t.Fatalf("recovered=%+v %t %v", active, found, err)
		}

		requireNoError(t, canonical.SetProjectionRetentionFloor(t.Context(), scope, "sqlite", 2), "%v")
		requireNoError(t, canonical.MarkProjectionDirty(t.Context(), active), "%v")
		requireNoError(t, runtime.Sync(t.Context(), scope), "%v")

		state, err := canonical.LoadScopeState(t.Context(), scope)
		if err != nil || state.Version != 2 || state.ChangeSeq != 2 {
			t.Fatalf("rebuild advanced history=%+v %v", state, err)
		}

		physical := applier.activeManifest(t, scope)
		if physical.Status != memory.ProjectionGenerationActive || physical.Watermark != 2 {
			t.Fatalf("physical=%+v", physical)
		}
	})
}
