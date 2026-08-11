package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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
