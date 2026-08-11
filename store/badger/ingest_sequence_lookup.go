package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.IngestSequenceStore = (*CanonicalStore)(nil)

func (s *CanonicalStore) LoadLatestIngestSequence(ctx context.Context, producerID string, scope memory.Scope) (memory.IngestSequenceCursor, bool, error) {
	store, release, err := openIngestSequenceStore(ctx, s, producerID, scope)
	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}
	defer release()
	key, err := badgerIngestCursorKey(scope, producerID)
	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}
	return loadStoredIngestSequenceCursor(store, key, producerID, scope)
}

func (s *CanonicalStore) LoadIngestSequence(ctx context.Context, producerID string, scope memory.Scope, sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	if sequence == 0 {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(memory.CodeInvalidDerived, "ingest sequence lookup is invalid", nil)
	}
	store, release, err := openIngestSequenceStore(ctx, s, producerID, scope)
	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}
	defer release()
	key, err := badgerIngestSequenceKey(scope, producerID, sequence)
	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}
	return loadStoredIngestSequenceRecord(store, key, producerID, scope, sequence)
}
