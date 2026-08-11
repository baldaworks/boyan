package badger

import (
	"errors"
	"fmt"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// RunValueLogGC performs at most one non-destructive Badger value-log cleanup.
// A skipped/rejected GC is normal when no reclaimable log exists.
func (s *CanonicalStore) RunValueLogGC(discardRatio float64) error {
	if s == nil || s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	if discardRatio <= 0 || discardRatio >= 1 {
		return memory.PermanentError(memory.CodeInvalidDerived, "badger GC discard ratio is invalid", nil)
	}
	s.gcMu.Lock()
	defer s.gcMu.Unlock()
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if err := s.db.RunValueLogGC(discardRatio); err != nil && !errors.Is(err, badger.ErrNoRewrite) {
		return canonicalStoreError("run canonical badger value-log GC", err)
	}
	return nil
}

// Close syncs and releases the canonical directory lock.
func (s *CanonicalStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if err := s.db.Sync(); err != nil {
		return fmt.Errorf("sync canonical badger store: %w", err)
	}
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close canonical badger store: %w", err)
	}
	s.db = nil
	return nil
}
