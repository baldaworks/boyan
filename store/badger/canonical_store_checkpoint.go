package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// LoadScopeCheckpoint loads the latest cursor for one trigger kind.
func (s *CanonicalStore) LoadScopeCheckpoint(ctx context.Context, scope memory.Scope, kind memory.ScopeCheckpointKind) (memory.ScopeCheckpoint, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	if err := kind.Validate(); err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	key, err := badgerProjectionCheckpointKey(scope, kind)
	if err != nil {
		return memory.ScopeCheckpoint{}, false, err
	}
	var checkpoint memory.ScopeCheckpoint
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordProjectionCheckpoint, &checkpoint)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.ScopeCheckpoint{}, false, nil
	}
	if err != nil {
		return memory.ScopeCheckpoint{}, false, canonicalStoreError("load scope checkpoint", err)
	}
	if err := checkpoint.Validate(); err != nil || checkpoint.Scope != scope || checkpoint.Kind != kind {
		return memory.ScopeCheckpoint{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored scope checkpoint is invalid", err)
	}
	return checkpoint, true, nil
}
