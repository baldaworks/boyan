package orderedingest

import (
	"context"
	"sync"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type memoryIngestSequenceStore struct {
	mu      sync.Mutex
	cursors map[string]boyan.IngestSequenceCursor
	records map[string]boyan.IngestSequenceRecord
}

func newMemoryIngestSequenceStore() *memoryIngestSequenceStore {
	return &memoryIngestSequenceStore{
		cursors: make(map[string]boyan.IngestSequenceCursor),
		records: make(map[string]boyan.IngestSequenceRecord),
	}
}

func (s *memoryIngestSequenceStore) LoadLatestIngestSequence(_ context.Context, producerID string, scope boyan.Scope) (boyan.IngestSequenceCursor, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor, ok := s.cursors[sequenceCursorKey(producerID, scope)]
	return cursor, ok, nil
}

func (s *memoryIngestSequenceStore) LoadIngestSequence(_ context.Context, producerID string, scope boyan.Scope, sequence uint64) (boyan.IngestSequenceRecord, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[sequenceRecordKey(producerID, scope, sequence)]
	return record, ok, nil
}

func (s *memoryIngestSequenceStore) SaveIngestSequence(_ context.Context, record boyan.IngestSequenceRecord) error {
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordKey := sequenceRecordKey(record.ProducerID, record.Scope, record.Sequence)
	if stored, ok := s.records[recordKey]; ok {
		if stored.Fingerprint != record.Fingerprint {
			return boyan.PermanentError(boyan.CodeConflict, "ingest sequence identity was reused", nil)
		}
		return nil
	}
	cursorKey := sequenceCursorKey(record.ProducerID, record.Scope)
	cursor, ok := s.cursors[cursorKey]
	switch {
	case !ok && record.Sequence != 1:
		return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
	case ok && record.Sequence != cursor.Latest+1:
		return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
	}
	s.records[recordKey] = record
	s.cursors[cursorKey] = boyan.IngestSequenceCursor{
		SchemaVersion: boyan.IngestSequenceSchemaVersionV1,
		ProducerID:    record.ProducerID,
		Scope:         record.Scope,
		Latest:        record.Sequence,
	}
	return nil
}

func sequenceCursorKey(producerID string, scope boyan.Scope) string {
	return producerID + "\x00" + string(scope.Kind) + "\x00" + scope.Key
}

func sequenceRecordKey(producerID string, scope boyan.Scope, sequence uint64) string {
	return sequenceCursorKey(producerID, scope) + "\x00" + time.Unix(int64(sequence), 0).UTC().Format(time.RFC3339Nano)
}
