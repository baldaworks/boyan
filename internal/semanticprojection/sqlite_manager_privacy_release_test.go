//nolint:cyclop,testpackage // Real terminal interleaving uses the private build hook and fixture.
package semanticprojection

import (
	"errors"
	"testing"

	sqlitestore "github.com/baldaworks/boyan/index/sqlite"
	"github.com/baldaworks/boyan/pkg/memory"
)

// Real scrub between Stage and publication must terminate this owner without
// creating a sticky cleanup failure that poisons a later healthy build.
func TestSQLiteManagerReleasesPrivacyRevokedCandidate(t *testing.T) {
	f := newSQLiteManagerFixture(t)

	a, err := sqlitestore.NewSQLiteCanonicalApplier(f.store.Store, f.canonical, f.privacy)
	if err != nil {
		t.Fatal(err)
	}

	a.ConfigureProjectionRetention(f.checkpoints, "sqlite")

	f.manager.beforeFinalCheck = func() {
		if err := a.ScrubCanonicalForget(t.Context(), f.scope, nil, []string{"revision-a"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.manager.Repair(t.Context(), f.scope); !errors.Is(err, ErrStaleView) {
		t.Fatalf("primary stale-view error lost: %v", err)
	}

	if f.store.releases.Load() != 1 {
		t.Fatal("owner did not reach terminal release")
	}

	f.assertFamilies(t, f.store.generation.BuildID, 0, 0, 0)

	status, err := f.manager.Status(t.Context(), f.scope)
	if err != nil || status.CleanupDeferred {
		t.Fatalf("cleanup falsely deferred=%v err=%v", status, err)
	}
	// Restore the synthetic fixture's still-eligible canonical projection using
	// its real applier; no SQL ready/complete fields are forged for recovery.
	f.manager.beforeFinalCheck = nil

	manifest := f.checkpoints.manifest
	if err := a.BeginProjectionRebuild(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	revisions := []memory.MemoryRevision{{RevisionID: "revision-a"}}
	if err := a.Apply(t.Context(), f.scope, manifest.GenerationID, nil, revisions); err != nil {
		t.Fatal(err)
	}

	if err := a.CommitProjectionManifest(t.Context(), manifest); err != nil {
		t.Fatal(err)
	}

	if err := a.Activate(t.Context(), f.scope, manifest.GenerationID); err != nil {
		t.Fatal(err)
	}

	if err := f.manager.Repair(t.Context(), f.scope); err != nil {
		t.Fatal(err)
	}

	status, err = f.manager.Status(t.Context(), f.scope)
	if err != nil || status.State != StateReady || status.CleanupDeferred {
		t.Fatalf("healthy rebuild status=%v err=%v", status, err)
	}
}
