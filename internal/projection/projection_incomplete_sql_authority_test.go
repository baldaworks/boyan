//go:build integration

package projection //nolint:testpackage // Actual native authority preserves incomplete recovery retention.

import (
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	"github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/postgres"
	"github.com/baldaworks/boyan/store/sqlite"
)

func TestProjectionIncompleteSQLAuthorityRetainsCandidatesUntilRecovery(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		owner := fixture.Store.(projectionTestOwner)
		requireNoError(t, owner.PrepareSourcePrivacy(t.Context()), "%v")

		var reader memory.RecallCanonicalReader

		switch store := fixture.Store.(type) {
		case *sqlite.Store:
			reader = sqlite.NewCanonicalReader(store)
		case *postgres.Store:
			reader = postgres.NewCanonicalReader(store)
		}

		applier := openProjectionSQLite(t, t.TempDir(), owner, reader)
		defer applier.Close()

		applier.ConfigureProjectionRetention(owner, "sqlite")

		scope := memory.Scope{Key: "test:incomplete-native", Kind: memory.ScopeKindPersonal}

		var current memory.ProjectionManifest

		for i, generation := range []string{"oldest", "previous", "current"} {
			manifest := memory.ProjectionManifest{Scope: scope, ProjectionID: "sqlite", GenerationID: generation,
				Status: memory.ProjectionGenerationDirty, UpdatedAt: time.Now().UTC().Add(-time.Duration(i) * time.Second)}
			requireNoError(t, owner.MarkProjectionDirty(t.Context(), manifest), "%v")
			requireNoError(t, owner.ActivateProjectionGeneration(t.Context(), manifest, manifest.UpdatedAt), "%v")

			if generation == "current" {
				current = manifest

				continue
			}

			requireNoError(t, applier.BeginProjectionRebuild(t.Context(), scope, generation), "%v")
			requireNoError(t, applier.CommitProjectionManifest(t.Context(), manifest), "%v")
			requireNoError(t, applier.AbortProjectionSync(t.Context(), generation), "%v")
		}

		requireNoError(t, applier.ReconcileProjection(t.Context()), "%v")

		if applier.countGeneration(t, "oldest") != 1 || applier.countGeneration(t, "previous") != 1 {
			t.Fatal("incomplete canonical active discarded recoverable superseded candidates")
		}

		requireNoError(t, applier.BeginProjectionRebuild(t.Context(), scope, current.GenerationID), "%v")
		requireNoError(t, applier.CommitProjectionManifest(t.Context(), current), "%v")
		requireNoError(t, applier.Activate(t.Context(), scope, current.GenerationID), "%v")

		if applier.countGeneration(t, "oldest") != 0 || applier.countGeneration(t, "previous") != 0 ||
			applier.countGeneration(t, "current") != 1 {
			t.Fatal("recovered native authority retained obsolete builds or lost current publication")
		}
	})
}
