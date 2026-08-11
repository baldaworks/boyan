package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadStoredIngestSequenceCursor(
	store *CanonicalStore,
	key []byte,
	producerID string,
	scope memory.Scope,
) (memory.IngestSequenceCursor, bool, error) {
	var cursor memory.IngestSequenceCursor
	err := store.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordIngestCursor, &cursor)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.IngestSequenceCursor{}, false, nil
	}
	if err != nil {
		return memory.IngestSequenceCursor{}, false, canonicalStoreError("load ingest sequence cursor", err)
	}
	if err := cursor.Validate(); err != nil || cursor.ProducerID != producerID || cursor.Scope != scope {
		return memory.IngestSequenceCursor{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored ingest sequence cursor is invalid", err)
	}
	return cursor, true, nil
}
