package badger

import (
	"context"
	"errors"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// SetProjectionRetentionFloor records a verified maintenance cursor. The
// floor is monotonic and is intentionally separate from disposable index
// files, so a projector can detect when incremental replay is no longer safe.
func (s *CanonicalStore) SetProjectionRetentionFloor(
	ctx context.Context,
	scope memory.Scope,
	projectionID string,
	floor uint64,
) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if err := scope.Validate(); err != nil || strings.TrimSpace(projectionID) == "" {
		return memory.PermanentError(memory.CodeInvalidScope, "projection retention update is invalid", err)
	}

	key, err := badgerProjectionRetentionKey(scope, projectionID)
	if err != nil {
		return err
	}

	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()

	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		return setProjectionRetentionFloor(txn, key, floor)
	})
	if err != nil {
		return canonicalStoreError("set projection retention floor", err)
	}

	return nil
}

func setProjectionRetentionFloor(txn *badger.Txn, key []byte, floor uint64) error {
	var current badgerProjectionRetention

	err := getCanonicalRecord(txn, key, badgerRecordProjectionRetention, &current)
	if err == nil && floor < current.Floor {
		return memory.PermanentError(memory.CodeConflict, "projection retention floor moved backwards", nil)
	}

	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}

	return putCanonicalRecord(txn, key, badgerRecordProjectionRetention, badgerProjectionRetention{Floor: floor})
}
