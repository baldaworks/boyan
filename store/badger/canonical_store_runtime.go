package badger

import (
	"errors"
	"fmt"
	"strings"

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

func canonicalStoreError(operation string, err error) error {
	if errors.Is(err, badger.ErrTxnTooBig) {
		return memory.PermanentError(memory.CodeLimitExceeded, operation, err)
	}
	if errors.Is(err, badger.ErrConflict) {
		return memory.RetryableError(memory.CodeConflict, operation, err)
	}
	var memoryError *memory.Error
	if errors.As(err, &memoryError) {
		return err
	}
	return memory.RetryableError(memory.CodeStoreFailure, operation, err)
}

// OpenCanonicalStore opens the sole writable canonical directory.
func OpenCanonicalStore(directory string) (*CanonicalStore, error) {
	return openWithConfig(directory, DefaultConfig())
}

func openWithConfig(directory string, config Config) (*CanonicalStore, error) {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return nil, fmt.Errorf("canonical badger directory is required")
	}
	if !config.SyncWrites {
		config.SyncWrites = true
	}
	if !config.DetectConflicts {
		config.DetectConflicts = true
	}
	if config.NumVersionsToKeep <= 0 {
		config.NumVersionsToKeep = 1
	}
	options := badger.DefaultOptions(directory).
		WithSyncWrites(config.SyncWrites).
		WithDetectConflicts(config.DetectConflicts).
		WithNumVersionsToKeep(config.NumVersionsToKeep)
	db, err := badger.Open(options)
	if err != nil {
		return nil, fmt.Errorf("open canonical badger store: %w", err)
	}
	return &CanonicalStore{db: db}, nil
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
