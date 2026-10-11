package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalStore) ListCanonicalRevisionRefs(
	ctx context.Context,
	scope memory.Scope,
	afterRevisionID string,
	limit uint32,
) ([]memory.RevisionRef, string, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, "", err
	}

	if err := validateCanonicalRevisionRefPage(scope, afterRevisionID, limit); err != nil {
		return nil, "", err
	}

	if s == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return nil, "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	prefix, err := canonicalPrefix(scope, badgerRecordRevision)
	if err != nil {
		return nil, "", err
	}

	refs, next, err := s.listCanonicalRevisionRefs(scope, prefix, afterRevisionID, limit)
	if err != nil {
		return nil, "", canonicalStoreError("list canonical revision refs", err)
	}

	if len(refs) < int(limit) {
		next = ""
	}

	return refs, next, nil
}
