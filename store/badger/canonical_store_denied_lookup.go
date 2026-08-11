package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) isDenied(ctx context.Context, scope memory.Scope, id, recordType, operation string, denied any) (bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return false, err
	}
	if s == nil {
		return false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := canonicalKey(scope, recordType, id)
	if err != nil {
		return false, err
	}
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, recordType, denied)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return false, nil
	}
	if err != nil {
		return false, canonicalStoreError(operation, err)
	}
	return true, nil
}
