package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// SaveScopeCheckpoint persists one latest-per-kind cursor and rejects stale
// writes. The maintenance mutex serializes this update with canonical
// lifecycle work in the same Badger owner.
func (s *CanonicalStore) SaveScopeCheckpoint(ctx context.Context, checkpoint memory.ScopeCheckpoint) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := checkpoint.Validate(); err != nil {
		return err
	}
	key, err := badgerProjectionCheckpointKey(checkpoint.Scope, checkpoint.Kind)
	if err != nil {
		return err
	}
	s.maintenanceMu.Lock()
	defer s.maintenanceMu.Unlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		var current memory.ScopeCheckpoint
		err := getCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, &current)
		if err == nil {
			if current.ScopeVersion > checkpoint.ScopeVersion || current.ChangeSeq > checkpoint.ChangeSeq || current.OccurredAt.After(checkpoint.OccurredAt) {
				return memory.PermanentError(memory.CodeConflict, "scope checkpoint moved backwards", nil)
			}
			if current.ScopeVersion == checkpoint.ScopeVersion && current.ChangeSeq == checkpoint.ChangeSeq && current.CheckpointID == checkpoint.CheckpointID {
				return nil
			}
		} else if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		return putCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, checkpoint)
	})
	if err != nil {
		return canonicalStoreError("save scope checkpoint", err)
	}
	return nil
}
