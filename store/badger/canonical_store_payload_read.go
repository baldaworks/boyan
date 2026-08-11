package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// LoadPayload reads a content-free reference's payload bytes.
func (s *CanonicalStore) LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := payloadRecordKey(ref)
	if err != nil {
		return nil, err
	}
	var data []byte
	if err := s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordPayload, &data)
	}); err != nil {
		return nil, canonicalStoreError("load payload", err)
	}
	if !isBadgerPayloadValid(data, ref) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "stored payload is invalid", nil)
	}
	return data, nil
}
