package badger

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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
		return activateProjectionGenerationTx(txn, manifest, manifestKey, activeKey, updatedAt)
	})
	if err != nil {
		return canonicalStoreError("activate projection generation", err)
	}
	return nil
}
