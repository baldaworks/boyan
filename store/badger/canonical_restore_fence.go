package badger

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/dgraph-io/badger/v4"
)

// CheckIntegrity preserves the ordinary-store lifecycle preflight before erase recovery.
func (s *CanonicalStore) CheckIntegrity(ctx context.Context) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if s == nil {
		return invalidSourcePrivacy()
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return invalidSourcePrivacy()
	}

	return nil
}

func (s *CanonicalStore) requireSourcePrivacyReady(txn *badger.Txn) error {
	return requireSourcePrivacyReady(txn)
}
func (s *CanonicalStore) rejectUnsupportedRestoreMarker() error {
	_, err := os.Lstat(filepath.Join(s.db.Opts().Dir, ".boyan-restore-privacy"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return invalidSourcePrivacy()
}
