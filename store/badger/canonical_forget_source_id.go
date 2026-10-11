package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (s *CanonicalStore) CanonicalSourceID(
	ctx context.Context,
	scope memory.Scope,
	source memory.SourceRef,
) (string, error) {
	if err := source.Validate(); err != nil {
		return "", err
	}

	if source.Scope != scope {
		return "", memory.PermanentError(memory.CodeScopeViolation, "canonical source scope does not match", nil)
	}

	if s == nil {
		return "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return "", memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	// Native turn sources have a deterministic identity. Check it first to
	// avoid scanning payload records in the common path.
	resolved, found, err := s.lookupNativeCanonicalSourceID(scope, source)
	if err != nil {
		return "", err
	}

	if found {
		return resolved, nil
	}

	resolved, err = s.scanCanonicalSourceID(scope, source)
	if err != nil {
		return "", err
	}

	if resolved != "" {
		return resolved, nil
	}

	return "", memory.PermanentError(memory.CodeNotFound, "canonical source does not exist", nil)
}
