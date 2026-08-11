package badger

import (
	"errors"
	"github.com/dgraph-io/badger/v4"
)

func putBadgerDenyRecord(txn *badger.Txn, key []byte, recordType string, value any) error {
	if _, err := txn.Get(key); err == nil {
		return nil
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return putCanonicalRecord(txn, key, recordType, value)
}
