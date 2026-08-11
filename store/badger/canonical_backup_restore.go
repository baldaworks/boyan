package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

const defaultBadgerRestorePendingWrites = 128

// RestoreCanonicalBackup loads a Badger backup into the current store. A
// caller must stop canonical writers before invoking this method and should
// run VerifyCanonicalIntegrity before making the restored store available.
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
	if err := s.db.Load(source, maxPendingWrites); err != nil {
		return canonicalStoreError("restore canonical backup", err)
	}
	if err := checkCanonicalMaintenanceContext(ctx); err != nil {
		return err
	}
	return nil
}

func normalizeBadgerRestorePendingWrites(maxPendingWrites int) (int, error) {
	if maxPendingWrites <= 0 {
		maxPendingWrites = defaultBadgerRestorePendingWrites
	}
	if maxPendingWrites > 4096 {
		return 0, memory.PermanentError(memory.CodeLimitExceeded, "canonical restore pending-write bound is too large", nil)
	}
	return maxPendingWrites, nil
}
