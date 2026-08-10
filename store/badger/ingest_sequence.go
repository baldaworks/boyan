package badger

import (
	"context"
	"errors"
	"reflect"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func isCanonicalProducerID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t")
}

var _ memory.IngestSequenceStore = (*CanonicalStore)(nil)

func (s *CanonicalStore) LoadLatestIngestSequence(ctx context.Context, producerID string, scope memory.Scope) (memory.IngestSequenceCursor, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}
	if !isCanonicalProducerID(producerID) {
		return memory.IngestSequenceCursor{}, false, memory.PermanentError(memory.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	if s == nil {
		return memory.IngestSequenceCursor{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.IngestSequenceCursor{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerIngestCursorKey(scope, producerID)
	if err != nil {
		return memory.IngestSequenceCursor{}, false, err
	}
	var cursor memory.IngestSequenceCursor
	err = s.db.View(func(txn *badger.Txn) error {
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

func (s *CanonicalStore) LoadIngestSequence(ctx context.Context, producerID string, scope memory.Scope, sequence uint64) (memory.IngestSequenceRecord, bool, error) {
	if err := canonicalContextError(ctx); err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}
	if !isCanonicalProducerID(producerID) || sequence == 0 {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(memory.CodeInvalidDerived, "ingest sequence lookup is invalid", nil)
	}
	if s == nil {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerIngestSequenceKey(scope, producerID, sequence)
	if err != nil {
		return memory.IngestSequenceRecord{}, false, err
	}
	var record memory.IngestSequenceRecord
	err = s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordIngestSequence, &record)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return memory.IngestSequenceRecord{}, false, nil
	}
	if err != nil {
		return memory.IngestSequenceRecord{}, false, canonicalStoreError("load ingest sequence record", err)
	}
	if err := record.Validate(); err != nil || record.ProducerID != producerID || record.Scope != scope || record.Sequence != sequence {
		return memory.IngestSequenceRecord{}, false, memory.PermanentError(memory.CodeStoreFailure, "stored ingest sequence record is invalid", err)
	}
	return record, true, nil
}

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
		var stored memory.IngestSequenceRecord
		lookupErr := getCanonicalRecord(txn, sequenceKey, badgerRecordIngestSequence, &stored)
		if lookupErr == nil {
			if err := stored.Validate(); err != nil {
				return err
			}
			if stored.Fingerprint != record.Fingerprint || !reflect.DeepEqual(stored.Outcome, record.Outcome) {
				return memory.PermanentError(memory.CodeConflict, "ingest sequence identity was reused", nil)
			}
			return nil
		}
		if !errors.Is(lookupErr, badger.ErrKeyNotFound) {
			return lookupErr
		}

		cursor := memory.IngestSequenceCursor{SchemaVersion: memory.IngestSequenceSchemaVersionV1, ProducerID: record.ProducerID, Scope: record.Scope}
		cursorErr := getCanonicalRecord(txn, cursorKey, badgerRecordIngestCursor, &cursor)
		if cursorErr != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
			return cursorErr
		}
		if err := cursor.Validate(); err != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
			return err
		}
		switch {
		case errors.Is(cursorErr, badger.ErrKeyNotFound):
			if record.Sequence != 1 {
				return memory.PermanentError(memory.CodeConflict, "ingest sequence is not next", nil)
			}
		case record.Sequence != cursor.Latest+1:
			return memory.PermanentError(memory.CodeConflict, "ingest sequence is not next", nil)
		}
		cursor.Latest = record.Sequence
		if err := putCanonicalRecord(txn, sequenceKey, badgerRecordIngestSequence, record); err != nil {
			return err
		}
		return putCanonicalRecord(txn, cursorKey, badgerRecordIngestCursor, cursor)
	})
	if err != nil {
		return canonicalStoreError("save ingest sequence record", err)
	}
	return nil
}
