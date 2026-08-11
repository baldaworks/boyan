package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) scanCanonicalSourceID(scope memory.Scope, source memory.SourceRef) (string, error) {
	prefix, err := canonicalPrefix(scope, badgerRecordSource)
	if err != nil {
		return "", err
	}
	var resolved string
	scanErr := s.db.View(func(txn *badger.Txn) error {
		iterator := txn.NewIterator(badger.DefaultIteratorOptions)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			var record memory.CanonicalSourceRecord
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordSource, &record); err != nil {
				return err
			}
			payloadKey, keyErr := canonicalKey(payloadScope(), badgerRecordPayload, record.Payload.ID)
			if keyErr != nil {
				return keyErr
			}
			var payload []byte
			if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
				return err
			}
			candidate, _, ok := canonicalSourceRef(payload, scope, record.SourceID)
			if ok && candidate == source {
				resolved = record.SourceID
				return nil
			}
		}
		return nil
	})
	if scanErr != nil {
		return "", canonicalStoreError("resolve canonical source identity", scanErr)
	}
	return resolved, nil
}
