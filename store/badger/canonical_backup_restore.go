package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultBadgerRestorePendingWrites = 128

// RestoreCanonicalBackup loads a Badger backup into the current store. A
// caller must stop canonical writers before invoking this method and should
// keep a failed destination unavailable. Restore checks record integrity before
// reporting success; Badger Load itself is not an atomic replacement.
func (s *CanonicalStore) RestoreCanonicalBackup(ctx context.Context, source io.Reader, maxPendingWrites int) error {
	if err := validateCanonicalMaintenance(ctx, source); err != nil {
		return err
	}

	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	maxPendingWrites, err := normalizeBadgerRestorePendingWrites(maxPendingWrites)
	if err != nil {
		return err
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	return s.restorePrivacyBackupLocked(ctx, source, maxPendingWrites)
}

func (s *CanonicalStore) restorePrivacyBackupLocked(ctx context.Context, source io.Reader, maxPendingWrites int) error {
	loadingRetry := s.restoreBlocked.Load() && s.restorePhase == restoreLoading
	if err := s.beginPrivacyRestore(); err != nil {
		return canonicalStoreError("fence canonical restore", err)
	}

	completed := false
	defer func() {
		if !completed {
			_ = s.unpublishRestoreReadiness()
		}
	}()

	if err := s.loadNativePrivacyBackup(ctx, source, maxPendingWrites, loadingRetry); err != nil {
		return canonicalStoreError("restore canonical backup", err)
	}

	if err := checkCanonicalMaintenanceContext(ctx); err != nil {
		return err
	}

	if err := s.completePrivacyRestore(ctx); err != nil {
		return canonicalStoreError("validate restored privacy", err)
	}

	completed = true

	return nil
}

func normalizeBadgerRestorePendingWrites(maxPendingWrites int) (int, error) {
	if maxPendingWrites <= 0 {
		maxPendingWrites = defaultBadgerRestorePendingWrites
	}

	if maxPendingWrites > maxCanonicalRestorePendingWrites {
		return 0, memory.PermanentError(memory.CodeLimitExceeded, "canonical restore pending-write bound is too large", nil)
	}

	return maxPendingWrites, nil
}
