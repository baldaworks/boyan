package canonicalsql

import (
	"context"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.ProjectionCheckpointStore = (*Owner)(nil)

type projectionActive struct {
	GenerationID string `json:"generation_id"`
}

func projectionManifestKey(scope memory.Scope, projectionID, generationID string) (Key, error) {
	key := Key{Family: ProjectionManifest, Scope: scope, Parts: [][]byte{[]byte(projectionID), []byte(generationID)}}
	_, _, err := key.values()

	return key, err
}

func validateProjectionLookup(ctx context.Context, scope memory.Scope, id string) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil || strings.TrimSpace(id) == "" {
		return memory.PermanentError(memory.CodeInvalidScope, "projection lookup is invalid", err)
	}

	return nil
}

// LoadProjectionManifest reads and validates an exact generation binding.
func (o *Owner) LoadProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID, generationID string) (memory.ProjectionManifest, bool, error) {
	if err := validateProjectionLookup(ctx, scope, projectionID); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	if strings.TrimSpace(generationID) == "" {
		return memory.ProjectionManifest{}, false, memory.PermanentError(
			memory.CodeInvalidScope,
			"projection manifest lookup is invalid",
			nil,
		)
	}

	var (
		manifest memory.ProjectionManifest
		found    bool
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		var err error

		manifest, found, err = loadProjectionManifest(ctx, tx, scope, projectionID, generationID)

		return err
	})

	return manifest, found, err
}

func loadProjectionManifest(ctx context.Context, tx *Tx, scope memory.Scope,
	projectionID, generationID string) (memory.ProjectionManifest, bool, error) {
	key, err := projectionManifestKey(scope, projectionID, generationID)
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	var manifest memory.ProjectionManifest
	if err := getPrivacyRecord(ctx, tx, key, ProjectionManifest, &manifest); privacyMissing(err) {
		return memory.ProjectionManifest{}, false, nil
	} else if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	if err := manifest.Validate(); err != nil || manifest.Scope != scope || manifest.ProjectionID != projectionID ||
		manifest.GenerationID != generationID {
		return memory.ProjectionManifest{}, false, proofStoreFailure("stored projection manifest is invalid", err)
	}

	return manifest, true, nil
}

// LoadActiveProjectionManifest resolves pointer and target in one snapshot.
// Dirty targets remain repairable, but cannot certify projection freshness.
func (o *Owner) LoadActiveProjectionManifest(ctx context.Context, scope memory.Scope,
	projectionID string) (memory.ProjectionManifest, bool, error) {
	if err := validateProjectionLookup(ctx, scope, projectionID); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	var (
		manifest memory.ProjectionManifest
		found    bool
	)

	err := o.View(ctx, func(ctx context.Context, tx *Tx) error {
		key, err := privacyKey(scope, ProjectionActive, projectionID)
		if err != nil {
			return err
		}

		var active projectionActive
		if err := getPrivacyRecord(ctx, tx, key, ProjectionActive, &active); privacyMissing(err) {
			return nil
		} else if err != nil {
			return err
		}

		manifest, found, err = loadProjectionManifest(ctx, tx, scope, projectionID, active.GenerationID)
		if err != nil || !found ||
			(manifest.Status != memory.ProjectionGenerationActive && manifest.Status != memory.ProjectionGenerationDirty) {
			return proofStoreFailure("active projection manifest is invalid", err)
		}

		return nil
	})
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	return manifest, found, nil
}

// MarkProjectionDirty persists the supplied generation as requiring repair.
func (o *Owner) MarkProjectionDirty(ctx context.Context, manifest memory.ProjectionManifest) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := manifest.Validate(); err != nil {
		return err
	}

	key, err := projectionManifestKey(manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil {
		return err
	}

	manifest.Status = memory.ProjectionGenerationDirty

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		return putPrivacyRecord(ctx, tx, key, ProjectionManifest, manifest)
	})
}
