package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) SaveIngestSequence(ctx context.Context, record memory.IngestSequenceRecord) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
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
	sequenceKey, err := badgerIngestSequenceKey(record.Scope, record.ProducerID, record.Sequence)
	if err != nil {
		return err
	}
	cursorKey, err := badgerIngestCursorKey(record.Scope, record.ProducerID)
	if err != nil {
		return err
	}
	err = s.db.Update(func(txn *badger.Txn) error {
		return saveIngestSequenceRecord(txn, sequenceKey, cursorKey, record)
	})
	if err != nil {
		return canonicalStoreError("save ingest sequence record", err)
	}
	return nil
}
