package badger

import (
	"errors"
	"reflect"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func saveIngestSequenceRecord(
	txn *badger.Txn,
	sequenceKey, cursorKey []byte,
	record memory.IngestSequenceRecord,
) error {
	var stored memory.IngestSequenceRecord

	lookupErr := getCanonicalRecord(txn, sequenceKey, badgerRecordIngestSequence, &stored)
	if lookupErr == nil {
		return validateIngestSequenceReplay(stored, record)
	}

	if !errors.Is(lookupErr, badger.ErrKeyNotFound) {
		return lookupErr
	}

	cursor := memory.IngestSequenceCursor{
		SchemaVersion: memory.IngestSequenceSchemaVersionV1,
		ProducerID:    record.ProducerID,
		Scope:         record.Scope,
	}

	cursorErr := getCanonicalRecord(txn, cursorKey, badgerRecordIngestCursor, &cursor)
	if cursorErr != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
		return cursorErr
	}

	if err := validateNextIngestSequence(cursor, cursorErr, record.Sequence); err != nil {
		return err
	}

	cursor.Latest = record.Sequence
	if err := putCanonicalRecord(txn, sequenceKey, badgerRecordIngestSequence, record); err != nil {
		return err
	}

	return putCanonicalRecord(txn, cursorKey, badgerRecordIngestCursor, cursor)
}

func validateIngestSequenceReplay(stored, record memory.IngestSequenceRecord) error {
	if err := stored.Validate(); err != nil {
		return err
	}

	if stored.Fingerprint != record.Fingerprint ||
		!reflect.DeepEqual(stored.Outcome, record.Outcome) {
		return memory.PermanentError(memory.CodeConflict, "ingest sequence identity was reused", nil)
	}

	return nil
}

func validateNextIngestSequence(cursor memory.IngestSequenceCursor, cursorErr error, sequence uint64) error {
	if err := cursor.Validate(); err != nil && !errors.Is(cursorErr, badger.ErrKeyNotFound) {
		return err
	}

	switch {
	case errors.Is(cursorErr, badger.ErrKeyNotFound):
		if sequence != 1 {
			return memory.PermanentError(memory.CodeConflict, "ingest sequence is not next", nil)
		}
	case sequence != cursor.Latest+1:
		return memory.PermanentError(memory.CodeConflict, "ingest sequence is not next", nil)
	}

	return nil
}
