package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

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
