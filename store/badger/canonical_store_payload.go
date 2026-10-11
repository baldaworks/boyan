package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// PutPayload persists payload bytes outside structural canonical records.
func (s *CanonicalStore) PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if !isBadgerPayloadValid(data, ref) {
		return memory.PermanentError(memory.CodeInvalidDerived, "payload is invalid", nil)
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

	if err := s.db.Update(func(txn *badger.Txn) error {
		return putCanonicalImmutableRecord(txn, key, badgerRecordPayload, data)
	}); err != nil {
		return canonicalStoreError("store payload", err)
	}

	return nil
}
