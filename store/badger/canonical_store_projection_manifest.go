package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

type badgerProjectionActive struct {
	GenerationID string `json:"generation_id"`
}

func (s *CanonicalStore) LoadProjectionManifest(
	ctx context.Context,
	scope memory.Scope,
	projectionID,
	generationID string,
) (memory.ProjectionManifest, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" ||
		strings.TrimSpace(generationID) == "" {
		return memory.ProjectionManifest{}, false, memory.PermanentError(
			memory.CodeInvalidScope,
			"projection manifest lookup is invalid",
			err,
		)
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

	if err := validateStoredProjectionManifest(manifest, scope, projectionID, generationID); err != nil {
		return memory.ProjectionManifest{}, false, err
	}

	return manifest, true, nil
}

func validateStoredProjectionManifest(
	manifest memory.ProjectionManifest,
	scope memory.Scope,
	projectionID,
	generationID string,
) error {
	if err := manifest.Validate(); err != nil || manifest.Scope != scope || manifest.ProjectionID != projectionID ||
		manifest.GenerationID != generationID {
		return memory.PermanentError(memory.CodeStoreFailure, "stored projection manifest is invalid", err)
	}

	return nil
}
