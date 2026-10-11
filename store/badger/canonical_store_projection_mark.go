package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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
