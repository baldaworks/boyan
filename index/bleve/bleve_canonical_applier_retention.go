package bleve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

type projectionManifestWalker interface {
	WalkProjectionManifests(
		ctx context.Context,
		projectionID string,
		visit func(
			memory.ProjectionManifest,
			bool,
		) error,
	) error
}

// ConfigureProjectionRetention enables pruning only when durable references can
// be enumerated. Plain portable checkpoint adapters remain supported.
func (a *BleveCanonicalApplier) ConfigureProjectionRetention(
	checkpoints memory.ProjectionCheckpointStore,
	projectionID string,
) {
	a.retention, _ = checkpoints.(projectionManifestWalker)
	a.projectionID = projectionID
}

// ReconcileProjection repairs committed marker switches before deleting orphans.
func (a *BleveCanonicalApplier) ReconcileProjection(ctx context.Context) error {
	if a.retention == nil {
		return nil
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	keep, active, err := a.retainedGenerations(ctx)
	if err != nil {
		return err
	}

	if err := a.resumeRetained(ctx, active); err != nil {
		return err
	}

	for id := range a.generations {
		keep[id] = true
	}

	root := a.retainPhysical(keep)

	return pruneGenerations(root, keep)
}

func (a *BleveCanonicalApplier) retainedGenerations(
	ctx context.Context,
) (
	map[string]bool,
	[]memory.ProjectionManifest,
	error,
) {
	keep := make(map[string]bool)
	previous := make(map[memory.Scope]memory.ProjectionManifest)
	active := make([]memory.ProjectionManifest, 0)

	if err := a.retention.WalkProjectionManifests(ctx, a.projectionID, func(
		manifest memory.ProjectionManifest,
		referenced bool,
	) error {
		if referenced {
			keep[manifest.GenerationID] = true
			active = append(active, manifest)
		}

		if manifest.Status == memory.ProjectionGenerationSuperseded {
			old := previous[manifest.Scope]
			if old.GenerationID == "" || old.UpdatedAt.Before(manifest.UpdatedAt) {
				previous[manifest.Scope] = manifest
			}
		}

		return nil
	}); err != nil {
		return nil, nil, err
	}

	for _, manifest := range previous {
		keep[manifest.GenerationID] = true
	}

	return keep, active, nil
}

func (a *BleveCanonicalApplier) resumeRetained(ctx context.Context, active []memory.ProjectionManifest) error {
	for _, manifest := range active {
		if manifest.Status == memory.ProjectionGenerationActive {
			if _, err := a.ResumeProjection(ctx, manifest.Scope, manifest.GenerationID); err != nil {
				return err
			}
		}
	}

	return nil
}

func (a *BleveCanonicalApplier) retainPhysical(keep map[string]bool) string {
	p := a.projection
	p.mu.RLock()

	if p.activeID != "" {
		keep[p.activeID] = true
	}

	for _, id := range p.activeScopeIDs {
		keep[id] = true
	}

	root := filepath.Join(p.root, bleveGenerationDirectory)
	p.mu.RUnlock()

	return root
}

func pruneGenerations(root string, keep map[string]bool) error {
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}

	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("bleve generation root is not a directory")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		id := entry.Name()
		if !entry.IsDir() || !validBleveGenerationID(id) || keep[id] {
			continue
		}

		if err := os.RemoveAll(filepath.Join(root, id)); err != nil {
			return fmt.Errorf("prune bleve generation: %w", err)
		}
	}

	return nil
}

// AbortProjectionSync retires failed sessions before pruning abandoned builders.
func (a *BleveCanonicalApplier) AbortProjectionSync(ctx context.Context, id string) error {
	a.mu.Lock()

	generation := a.generations[id]
	if generation != nil {
		if err := generation.Close(); err != nil {
			a.mu.Unlock()

			return err
		}
	}

	delete(a.generations, id)
	a.mu.Unlock()

	return a.ReconcileProjection(ctx)
}
