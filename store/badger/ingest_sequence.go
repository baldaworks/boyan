package badger

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"github.com/baldaworks/boyan"
	"github.com/dgraph-io/badger/v4"
)

func isCanonicalProducerID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsAny(value, "\r\n\t")
}

var _ boyan.IngestSequenceStore = (*BadgerSessionMemoryStore)(nil)

func (s *BadgerSessionMemoryStore) LoadLatestIngestSequence(ctx context.Context, producerID string, scope boyan.Scope) (boyan.IngestSequenceCursor, bool, error) {
	if err := sessionMemoryContextError(ctx); err != nil {
		return boyan.IngestSequenceCursor{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return boyan.IngestSequenceCursor{}, false, err
	}
	if !isCanonicalProducerID(producerID) {
		return boyan.IngestSequenceCursor{}, false, boyan.PermanentError(boyan.CodeInvalidDerived, "ingest producer id is invalid", nil)
	}
	if s == nil {
		return boyan.IngestSequenceCursor{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return boyan.IngestSequenceCursor{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerIngestCursorKey(scope, producerID)
	if err != nil {
		return boyan.IngestSequenceCursor{}, false, err
	}
	var cursor boyan.IngestSequenceCursor
	err = s.db.View(func(txn *badger.Txn) error {
		return getBadgerSessionMemoryRecord(txn, key, badgerRecordIngestCursor, &cursor)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return boyan.IngestSequenceCursor{}, false, nil
	}
	if err != nil {
		return boyan.IngestSequenceCursor{}, false, badgerSessionMemoryError("load ingest sequence cursor", err)
	}
	if err := cursor.Validate(); err != nil || cursor.ProducerID != producerID || cursor.Scope != scope {
		return boyan.IngestSequenceCursor{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "stored ingest sequence cursor is invalid", err)
	}
	return cursor, true, nil
}

func (s *BadgerSessionMemoryStore) LoadIngestSequence(ctx context.Context, producerID string, scope boyan.Scope, sequence uint64) (boyan.IngestSequenceRecord, bool, error) {
	if err := sessionMemoryContextError(ctx); err != nil {
		return boyan.IngestSequenceRecord{}, false, err
	}
	if err := scope.Validate(); err != nil {
		return boyan.IngestSequenceRecord{}, false, err
	}
	if !isCanonicalProducerID(producerID) || sequence == 0 {
		return boyan.IngestSequenceRecord{}, false, boyan.PermanentError(boyan.CodeInvalidDerived, "ingest sequence lookup is invalid", nil)
	}
	if s == nil {
		return boyan.IngestSequenceRecord{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return boyan.IngestSequenceRecord{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	key, err := badgerIngestSequenceKey(scope, producerID, sequence)
	if err != nil {
		return boyan.IngestSequenceRecord{}, false, err
	}
	var record boyan.IngestSequenceRecord
	err = s.db.View(func(txn *badger.Txn) error {
		return getBadgerSessionMemoryRecord(txn, key, badgerRecordIngestSequence, &record)
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return boyan.IngestSequenceRecord{}, false, nil
	}
	if err != nil {
		return boyan.IngestSequenceRecord{}, false, badgerSessionMemoryError("load ingest sequence record", err)
	}
	if err := record.Validate(); err != nil || record.ProducerID != producerID || record.Scope != scope || record.Sequence != sequence {
		return boyan.IngestSequenceRecord{}, false, boyan.PermanentError(boyan.CodeStoreFailure, "stored ingest sequence record is invalid", err)
	}
	return record, true, nil
}

func (s *BadgerSessionMemoryStore) SaveIngestSequence(ctx context.Context, record boyan.IngestSequenceRecord) error {
	if err := sessionMemoryContextError(ctx); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	if s == nil {
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
	}
	s.maintenanceMu.RLock()
	defer s.maintenanceMu.RUnlock()
	if s.db == nil {
		return boyan.PermanentError(boyan.CodeStoreFailure, "canonical badger store is closed", nil)
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
		var stored boyan.IngestSequenceRecord
		lookupErr := getBadgerSessionMemoryRecord(txn, sequenceKey, badgerRecordIngestSequence, &stored)
		if lookupErr == nil {
			if err := stored.Validate(); err != nil {
				return err
			}
			if stored.Fingerprint != record.Fingerprint || !reflect.DeepEqual(stored.Outcome, record.Outcome) {
				return boyan.PermanentError(boyan.CodeConflict, "ingest sequence identity was reused", nil)
			}
			return nil
		}
		if !errors.Is(lookupErr, badger.ErrKeyNotFound) {
			return lookupErr
		}

		cursor := boyan.IngestSequenceCursor{SchemaVersion: boyan.IngestSequenceSchemaVersionV1, ProducerID: record.ProducerID, Scope: record.Scope}
		cursorErr := getBadgerSessionMemoryRecord(txn, cursorKey, badgerRecordIngestCursor, &cursor)
		if cursorErr != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
			return cursorErr
		}
		if err := cursor.Validate(); err != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
			return err
		}
		switch {
		case errors.Is(cursorErr, badger.ErrKeyNotFound):
			if record.Sequence != 1 {
				return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
			}
		case record.Sequence != cursor.Latest+1:
			return boyan.PermanentError(boyan.CodeConflict, "ingest sequence is not next", nil)
		}
		cursor.Latest = record.Sequence
		if err := putBadgerSessionMemoryRecord(txn, sequenceKey, badgerRecordIngestSequence, record); err != nil {
			return err
		}
		return putBadgerSessionMemoryRecord(txn, cursorKey, badgerRecordIngestCursor, cursor)
	})
	if err != nil {
		return badgerSessionMemoryError("save ingest sequence record", err)
	}
	return nil
}
