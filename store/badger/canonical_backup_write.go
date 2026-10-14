package badger

import (
	"context"
	"io"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// WriteCanonicalBackup writes a full or incremental Badger backup. The store
// lock serializes backup/restore operations with each other; application
// lifecycle code must quiesce mutation workers before restore as required by
// Badger's DB.Load contract.
func (s *CanonicalStore) WriteCanonicalBackup(
	ctx context.Context,
	destination io.Writer,
	since uint64,
) (uint64, error) {
	if err := validateCanonicalMaintenance(ctx, destination); err != nil {
		return 0, err
	}

	if s == nil {
		return 0, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return 0, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	if s.restoreBlocked.Load() {
		return 0, restoreUnavailable()
	}

	version, err := s.db.Backup(destination, since)
	if err != nil {
		return 0, canonicalStoreError("write canonical backup", err)
	}

	return version, nil
}
