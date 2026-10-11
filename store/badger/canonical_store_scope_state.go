package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// LoadScopeState reads the small mutable cursor for one exact scope.
func (s *CanonicalStore) LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.ScopeState{}, err
	}

	if err := scope.Validate(); err != nil {
		return memory.ScopeState{}, err
	}

	if s == nil {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()

	if s.db == nil {
		return memory.ScopeState{}, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}

	key, err := badgerScopeKey(scope)
	if err != nil {
		return memory.ScopeState{}, err
	}

	state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope}

	err = s.db.View(func(txn *badger.Txn) error {
		err := getCanonicalRecord(txn, key, badgerRecordScope, &state)
		if errors.Is(err, badger.ErrKeyNotFound) {
			return nil
		}

		return err
	})
	if err != nil {
		return memory.ScopeState{}, canonicalStoreError("load canonical scope state", err)
	}

	if err := state.Validate(); err != nil || state.Scope != scope {
		return memory.ScopeState{}, memory.PermanentError(
			memory.CodeStoreFailure,
			"stored canonical scope state is invalid",
			err,
		)
	}

	return state, nil
}
