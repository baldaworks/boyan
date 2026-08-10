package badger

import (
	"context"
	"errors"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerProjectionActive struct {
	GenerationID string `json:"generation_id"`
}

type badgerProjectionRetention struct {
	Floor uint64 `json:"floor"`
}

func (s *CanonicalStore) LoadProjectionManifest(ctx context.Context, scope memory.Scope, projectionID, generationID string) (memory.ProjectionManifest, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ProjectionManifest{}, false, err
	}
	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" || strings.TrimSpace(generationID) == "" {
		return memory.ProjectionManifest{}, false, memory.PermanentError(memory.CodeInvalidScope, "projection manifest lookup is invalid", err)
	}
	key, err := badgerProjectionManifestKey(scope, projectionID, generationID)
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}
	var manifest memory.ProjectionManifest
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordProjectionManifest, &manifest)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ProjectionManifest{}, false, nil
	}
	if err != nil {
		return memory.ProjectionManifest{}, false, canonicalStoreError("load projection manifest", err)
	}
	if err := manifest.Validate(); err != nil || manifest.Scope != scope || manifest.ProjectionID != projectionID || manifest.GenerationID != generationID {
		return memory.ProjectionManifest{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored projection manifest is invalid", err)
	}
	return manifest, true, nil
}

func (s *CanonicalStore) LoadActiveProjectionManifest(ctx context.Context, scope memory.Scope, projectionID string) (memory.ProjectionManifest, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ProjectionManifest{}, false, err
	}
	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" {
		return memory.ProjectionManifest{}, false, memory.PermanentError(memory.CodeInvalidScope, "active projection lookup is invalid", err)
	}
	activeKey, err := canonicalKey(scope, badgerRecordProjectionActive, projectionID)
	if err != nil {
		return memory.ProjectionManifest{}, false, err
	}
	var active badgerProjectionActive
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, activeKey, badgerRecordProjectionActive, &active)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ProjectionManifest{}, false, nil
	}
	if err != nil {
		return memory.ProjectionManifest{}, false, canonicalStoreError("load active projection", err)
	}
	manifest, found, err := s.LoadProjectionManifest(ctx, scope, projectionID, active.GenerationID)
	if err != nil || !found || manifest.Status != memory.ProjectionGenerationActive {
		return memory.ProjectionManifest{}, false, memory.PermanentError(memory.CodeStoreFailure, "active projection manifest is invalid", err)
	}
	return manifest, true, nil
}

func (s *CanonicalStore) MarkProjectionDirty(ctx context.Context, manifest memory.ProjectionManifest) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil {
		return err
	}
	manifest.Status = memory.ProjectionGenerationDirty
	key, err := badgerProjectionManifestKey(manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil {
		return err
	}
	if err := s.db.Update(func(txn *badger.Txn) error {
		return putCanonicalRecord(txn, key, badgerRecordProjectionManifest, manifest)
	}); err != nil {
		return canonicalStoreError("mark projection generation dirty", err)
	}
	return nil
}

func (s *CanonicalStore) AdvanceProjectionWatermark(ctx context.Context, manifest memory.ProjectionManifest, watermark uint64, updatedAt time.Time) error {
	return s.updateProjectionManifest(ctx, manifest, updatedAt, func(stored *memory.ProjectionManifest) error {
		if stored.Status != memory.ProjectionGenerationDirty || watermark < stored.Watermark {
			return memory.PermanentError(memory.CodeConflict, "projection watermark cannot advance", nil)
		}
		stored.Watermark = watermark
		return nil
	})
}

func (s *CanonicalStore) ActivateProjectionGeneration(ctx context.Context, manifest memory.ProjectionManifest, updatedAt time.Time) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil || updatedAt.IsZero() {
		return memory.PermanentError(memory.CodePermanent, "projection activation is invalid", err)
	}
	manifestKey, err := badgerProjectionManifestKey(manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil {
		return err
	}
	activeKey, err := canonicalKey(manifest.Scope, badgerRecordProjectionActive, manifest.ProjectionID)
	if err != nil {
		return err
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		var stored memory.ProjectionManifest
		if err := getCanonicalRecord(txn, manifestKey, badgerRecordProjectionManifest, &stored); err != nil {
			return err
		}
		if stored.Status != memory.ProjectionGenerationDirty {
			return memory.PermanentError(memory.CodeConflict, "projection generation is not dirty", nil)
		}
		var previous badgerProjectionActive
		if err := getCanonicalRecord(txn, activeKey, badgerRecordProjectionActive, &previous); err == nil && previous.GenerationID != stored.GenerationID {
			previousKey, keyErr := badgerProjectionManifestKey(manifest.Scope, manifest.ProjectionID, previous.GenerationID)
			if keyErr != nil {
				return keyErr
			}
			var old memory.ProjectionManifest
			if err := getCanonicalRecord(txn, previousKey, badgerRecordProjectionManifest, &old); err != nil {
				return err
			}
			old.Status = memory.ProjectionGenerationSuperseded
			old.UpdatedAt = updatedAt.UTC()
			if err := putCanonicalRecord(txn, previousKey, badgerRecordProjectionManifest, old); err != nil {
				return err
			}
		} else if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		stored.Status = memory.ProjectionGenerationActive
		stored.UpdatedAt = updatedAt.UTC()
		if err := putCanonicalRecord(txn, manifestKey, badgerRecordProjectionManifest, stored); err != nil {
			return err
		}
		return putCanonicalRecord(txn, activeKey, badgerRecordProjectionActive, badgerProjectionActive{GenerationID: stored.GenerationID})
	})
	if err != nil {
		return canonicalStoreError("activate projection generation", err)
	}
	return nil
}

func (s *CanonicalStore) updateProjectionManifest(ctx context.Context, manifest memory.ProjectionManifest, updatedAt time.Time, update func(*memory.ProjectionManifest) error) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := manifest.Validate(); err != nil || updatedAt.IsZero() {
		return memory.PermanentError(memory.CodePermanent, "projection manifest update is invalid", err)
	}
	key, err := badgerProjectionManifestKey(manifest.Scope, manifest.ProjectionID, manifest.GenerationID)
	if err != nil {
		return err
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		var stored memory.ProjectionManifest
		if err := getCanonicalRecord(txn, key, badgerRecordProjectionManifest, &stored); err != nil {
			return err
		}
		if stored.Scope != manifest.Scope || stored.ProjectionID != manifest.ProjectionID || stored.GenerationID != manifest.GenerationID {
			return memory.PermanentError(memory.CodeConflict, "projection generation changed", nil)
		}
		if err := update(&stored); err != nil {
			return err
		}
		stored.UpdatedAt = updatedAt.UTC()
		return putCanonicalRecord(txn, key, badgerRecordProjectionManifest, stored)
	})
	if err != nil {
		return canonicalStoreError("update projection manifest", err)
	}
	return nil
}

// LoadProjectionRetentionFloor returns the minimum watermark that remains safe
// for incremental replay after retention removed older change records. A
// missing floor means that all canonical changes are still available and is
// represented as zero.
func (s *CanonicalStore) LoadProjectionRetentionFloor(ctx context.Context, scope memory.Scope, projectionID string) (uint64, error) {
	if err := canonicalContextError(ctx); err != nil {
		return 0, err
	}
	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" {
		return 0, memory.PermanentError(memory.CodeInvalidScope, "projection retention lookup is invalid", err)
	}
	key, err := badgerProjectionRetentionKey(scope, projectionID)
	if err != nil {
		return 0, err
	}
	var retention badgerProjectionRetention
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordProjectionRetention, &retention)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, canonicalStoreError("load projection retention floor", err)
	}
	return retention.Floor, nil
}

// SetProjectionRetentionFloor records a verified maintenance cursor. The
// floor is monotonic and is intentionally separate from disposable index
// files, so a projector can detect when incremental replay is no longer safe.
func (s *CanonicalStore) SetProjectionRetentionFloor(ctx context.Context, scope memory.Scope, projectionID string, floor uint64) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" {
		return memory.PermanentError(memory.CodeInvalidScope, "projection retention update is invalid", err)
	}
	key, err := badgerProjectionRetentionKey(scope, projectionID)
	if err != nil {
		return err
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		var current badgerProjectionRetention
		err := getCanonicalRecord(txn, key, badgerRecordProjectionRetention, &current)
		if err == nil && floor < current.Floor {
			return memory.PermanentError(memory.CodeConflict, "projection retention floor moved backwards", nil)
		}
		if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return putCanonicalRecord(txn, key, badgerRecordProjectionRetention, badgerProjectionRetention{Floor: floor})
	})
	if err != nil {
		return canonicalStoreError("set projection retention floor", err)
	}
	return nil
}

// LoadScopeCheckpoint loads the latest cursor for one trigger kind.
func (s *CanonicalStore) LoadScopeCheckpoint(ctx context.Context, scope memory.Scope, kind memory.ScopeCheckpointKind) (memory.ScopeCheckpoint, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	if err := kind.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	key, err := badgerProjectionCheckpointKey(scope, kind)
	if err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	var checkpoint memory.ScopeCheckpoint
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, &checkpoint)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ScopeCheckpoint{}, false, nil
	}
	if err != nil {
		return memory.ScopeCheckpoint{}, false, canonicalStoreError("load scope checkpoint", err)
	}
	if err := checkpoint.Validate(); err != nil || checkpoint.Scope != scope || checkpoint.Kind != kind {
		return memory.ScopeCheckpoint{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored scope checkpoint is invalid", err)
	}
	return checkpoint, true, nil
}

// SaveScopeCheckpoint persists one latest-per-kind cursor and rejects stale
// writes. The maintenance mutex serializes this update with canonical
// lifecycle work in the same Badger owner.
func (s *CanonicalStore) SaveScopeCheckpoint(ctx context.Context, checkpoint memory.ScopeCheckpoint) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	key, err := badgerProjectionCheckpointKey(checkpoint.Scope, checkpoint.Kind)
	if err != nil {
		return err
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		var current memory.ScopeCheckpoint
		err := getCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, &current)
		if err == nil {
			if current.ScopeVersion > checkpoint.ScopeVersion || current.ChangeSeq > checkpoint.ChangeSeq || current.OccurredAt.After(checkpoint.OccurredAt) {
				return memory.PermanentError(memory.CodeConflict, "scope checkpoint moved backwards", nil)
			}
			if current.ScopeVersion == checkpoint.ScopeVersion && current.ChangeSeq == checkpoint.ChangeSeq && current.CheckpointID == checkpoint.CheckpointID {
				return nil
			}
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return putCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, checkpoint)
	})
	if err != nil {
		return canonicalStoreError("save scope checkpoint", err)
	}
	return nil
}
