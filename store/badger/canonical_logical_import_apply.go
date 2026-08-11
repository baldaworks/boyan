package badger

import (
	"bytes"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func applyCanonicalLogicalImportBatch(txn *badger.Txn, records []CanonicalLogicalExportRecord) error {
	for _, record := range records {
		key, err := encodeCanonicalLogicalKey(record.Components, record.Suffix)
		if err != nil {
			return err
		}
		item, err := txn.Get(key)
		if err == nil {
			existing, copyErr := item.ValueCopy(nil)
			if copyErr != nil {
				return copyErr
			}
			if !bytes.Equal(existing, record.Envelope) {
				return memory.PermanentError(memory.CodeConflict, "canonical logical import collides with existing record", nil)
			}
			continue
		}
		if !errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}
		if err := txn.Set(key, record.Envelope); err != nil {
			return err
		}
	}
	return nil
}
