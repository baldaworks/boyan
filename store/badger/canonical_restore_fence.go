package badger

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

const restoreFenceName = ".boyan-restore-privacy"
const restoreLoading = "v1 loading\n"
const restoreValidated = "v1 validated\n"

func restoreUnavailable() error {
	return memory.RetryableError(memory.CodeUnavailable, "canonical restore privacy is unavailable", nil)
}

func (s *CanonicalStore) requireSourcePrivacyReady(txn *badger.Txn) error {
	if s.restoreBlocked.Load() {
		return restoreUnavailable()
	}

	return requireSourcePrivacyReady(txn)
}

func (s *CanonicalStore) restoreFencePath() string {
	return filepath.Join(s.db.Opts().Dir, restoreFenceName)
}

func (s *CanonicalStore) loadRestoreFence() error {
	path := s.restoreFencePath()

	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil || info.Size() > 64 {
		return invalidSourcePrivacy()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return invalidSourcePrivacy()
	}

	phase := string(data)
	if phase != restoreLoading && phase != restoreValidated {
		return invalidSourcePrivacy()
	}

	s.restorePhase = phase
	s.restoreBlocked.Store(true)

	return s.unpublishRestoreReadiness()
}

func (s *CanonicalStore) unpublishRestoreReadiness() error {
	return s.db.Update(func(txn *badger.Txn) error { return txn.Delete(sourcePrivacyReadinessKey()) })
}

func (s *CanonicalStore) beginPrivacyRestore() error {
	s.restoreBlocked.Store(true)

	if err := s.unpublishRestoreReadiness(); err != nil {
		return err
	}

	return s.writeRestoreFence(restoreLoading)
}

func (s *CanonicalStore) writeRestoreFence(phase string) error {
	if s.beforeRestoreFenceWrite != nil {
		if err := s.beforeRestoreFenceWrite(phase); err != nil {
			return err
		}
	}

	path := s.restoreFencePath()

	file, err := os.CreateTemp(filepath.Dir(path), ".boyan-restore-privacy-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())

	if _, err := file.WriteString(phase); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()

		return err
	}

	if err := file.Close(); err != nil {
		return err
	}

	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}

	if err := syncRestoreDirectory(filepath.Dir(path)); err != nil {
		return err
	}

	s.restorePhase = phase

	return nil
}

func syncRestoreDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()

	return directory.Sync()
}

func (s *CanonicalStore) completePrivacyRestore(ctx context.Context) error {
	if err := s.db.View(func(txn *badger.Txn) error { return verifyCanonicalIntegrityTxn(ctx, txn) }); err != nil {
		return err
	}

	if err := s.syncPrivacyRestoreDB(); err != nil {
		return err
	}

	if err := s.writeRestoreFence(restoreValidated); err != nil {
		return err
	}

	return s.prepareValidatedPrivacyRestore(ctx)
}

func (s *CanonicalStore) resumeValidatedPrivacyRestore(ctx context.Context) error {
	if s.restorePhase != restoreValidated {
		return restoreUnavailable()
	}

	if err := s.db.View(func(txn *badger.Txn) error { return verifyCanonicalIntegrityTxn(ctx, txn) }); err != nil {
		return err
	}

	return s.prepareValidatedPrivacyRestore(ctx)
}

func (s *CanonicalStore) prepareValidatedPrivacyRestore(ctx context.Context) error {
	if err := s.prepareSourcePrivacyLocked(ctx, true); err != nil {
		return err
	}

	if err := s.syncPrivacyRestoreDB(); err != nil {
		return err
	}

	if err := os.Remove(s.restoreFencePath()); err != nil {
		return err
	}

	if err := syncRestoreDirectory(s.db.Opts().Dir); err != nil {
		return err
	}

	s.restorePhase = ""
	s.restoreBlocked.Store(false)

	return nil
}

func (s *CanonicalStore) syncPrivacyRestoreDB() error {
	if s.beforeRestoreDBSync != nil {
		if err := s.beforeRestoreDBSync(); err != nil {
			return err
		}
	}

	return s.db.Sync()
}
