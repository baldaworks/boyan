package badger

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalStoreProjectionManifestFailsClosedUntilActivation(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	manifest := boyan.ProjectionManifest{Scope: boyan.Scope{Key: "canonical:projection-manifest", Kind: boyan.ScopeKindPersonal}, ProjectionID: "bleve", GenerationID: "generation-1", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now}
	if err := store.MarkProjectionDirty(context.Background(), manifest); err != nil {
		t.Fatalf("MarkProjectionDirty() error = %v", err)
	}
	stored, found, err := store.LoadProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil || !found || stored.Status != boyan.ProjectionGenerationDirty {
		t.Fatalf("LoadProjectionManifest(dirty) = %#v, %t, %v", stored, found, err)
	}
	if err := store.AdvanceProjectionWatermark(context.Background(), manifest, 7, now.Add(time.Second)); err != nil {
		t.Fatalf("AdvanceProjectionWatermark() error = %v", err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), manifest, now.Add(2*time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration() error = %v", err)
	}
	stored, found, err = store.LoadProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil || !found || stored.Status != boyan.ProjectionGenerationActive || stored.Watermark != 7 {
		t.Fatalf("LoadProjectionManifest(active) = %#v, %t, %v", stored, found, err)
	}
	active, found, err := store.LoadActiveProjectionManifest(context.Background(), manifest.Scope, manifest.ProjectionID)
	if err != nil || !found || active.GenerationID != manifest.GenerationID {
		t.Fatalf("LoadActiveProjectionManifest() = %#v, %t, %v", active, found, err)
	}
}

func TestCanonicalStoreAtomicallySwitchesActiveProjectionGeneration(t *testing.T) {
	store, err := OpenCanonicalStore(filepath.Join(t.TempDir(), "memory.badger"))
	if err != nil {
		t.Fatalf("OpenCanonicalStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Date(2026, time.August, 6, 0, 0, 0, 0, time.UTC)
	scope := boyan.Scope{Key: "canonical:projection-switch", Kind: boyan.ScopeKindPersonal}
	first := boyan.ProjectionManifest{Scope: scope, ProjectionID: "bleve", GenerationID: "generation-1", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now}
	if err := store.MarkProjectionDirty(context.Background(), first); err != nil {
		t.Fatalf("MarkProjectionDirty(first) error = %v", err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), first, now.Add(time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration(first) error = %v", err)
	}
	second := boyan.ProjectionManifest{Scope: scope, ProjectionID: "bleve", GenerationID: "generation-2", Status: boyan.ProjectionGenerationBuilding, UpdatedAt: now.Add(2 * time.Second)}
	if err := store.MarkProjectionDirty(context.Background(), second); err != nil {
		t.Fatalf("MarkProjectionDirty(second) error = %v", err)
	}
	active, found, err := store.LoadActiveProjectionManifest(context.Background(), scope, "bleve")
	if err != nil || !found || active.GenerationID != first.GenerationID {
		t.Fatalf("active before switch = %#v, %t, %v", active, found, err)
	}
	if err := store.ActivateProjectionGeneration(context.Background(), second, now.Add(3*time.Second)); err != nil {
		t.Fatalf("ActivateProjectionGeneration(second) error = %v", err)
	}
	active, found, err = store.LoadActiveProjectionManifest(context.Background(), scope, "bleve")
	if err != nil || !found || active.GenerationID != second.GenerationID {
		t.Fatalf("active after switch = %#v, %t, %v", active, found, err)
	}
	old, found, err := store.LoadProjectionManifest(context.Background(), scope, "bleve", first.GenerationID)
	if err != nil || !found || old.Status != boyan.ProjectionGenerationSuperseded {
		t.Fatalf("old generation = %#v, %t, %v", old, found, err)
	}
}
