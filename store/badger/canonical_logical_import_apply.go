package badger

import (
	"bytes"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func applyCanonicalLogicalImportBatch(txn *badger.Txn, records []CanonicalLogicalExportRecord) error {
	for _, record := range records {
		if err := applyCanonicalLogicalImportRecord(txn, record); err != nil {
			return err
		}
	}

	return nil
}

func applyCanonicalLogicalImportRecord(txn *badger.Txn, record CanonicalLogicalExportRecord) error {
	key, err := encodeCanonicalLogicalKey(record.Components, record.Suffix)
	if err != nil {
		return err
	}

	if isCanonicalPrivacyRecord(record.RecordType) {
		return applyLogicalPrivacyRecord(txn, key, record)
	}

	item, err := txn.Get(key)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return txn.Set(key, record.Envelope)
	}

	if err != nil {
		return err
	}

	if item.ValueSize() > maxCanonicalLogicalLineBytes {
		return invalidSourcePrivacy()
	}

	existing, err := item.ValueCopy(nil)
	if err != nil {
		return err
	}

	if !bytes.Equal(existing, record.Envelope) {
		return memory.PermanentError(memory.CodeConflict, "canonical logical import collides with existing record", nil)
	}

	return nil
}

func applyLogicalPrivacyRecord(txn *badger.Txn, key []byte, record CanonicalLogicalExportRecord) error {
	if record.RecordType == badgerRecordSourcePrivacyReady {
		return nil
	}

	envelope, err := memory.UnmarshalRecordEnvelope(record.Envelope)
	if err != nil {
		return err
	}

	merged, err := mergeNativePrivacyRecord(txn, key, record.Components, envelope, false)
	if err != nil {
		return err
	}

	return txn.Set(key, merged)
}
