package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadStoredIngestSequenceRecord(
	store *CanonicalStore,
	key []byte,
	producerID string,
	scope memory.Scope,
	sequence uint64,
) (memory.IngestSequenceRecord, bool, error) {
	var record memory.IngestSequenceRecord

	err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordIngestSequence, &record)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.IngestSequenceRecord{}, false, nil
	}

	if err != nil {
		return memory.IngestSequenceRecord{}, false, canonicalStoreError("load ingest sequence record", err)
	}

	if err := record.Validate(); err != nil || record.ProducerID != producerID || record.Scope != scope ||
		record.Sequence != sequence {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(
			memory.CodeStoreFailure,
			"stored ingest sequence record is invalid",
			err,
		)
	}

	return record, true, nil
}
