package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// DeletePayload removes a payload blob after its logical forget state has
// committed. Structural tombstones remain intact.
func (s *CanonicalStore) DeletePayload(ctx context.Context, ref memory.PayloadRef) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	if s == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := payloadRecordKey(ref)
	if err != nil {
		return err
	}
	if err := s.db.Update(func(txn *badger.Txn) error { return txn.Delete(key) }); err != nil {
		return canonicalStoreError("delete payload", err)
	}
	return nil
}
