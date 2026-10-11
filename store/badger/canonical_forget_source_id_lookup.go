package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) lookupNativeCanonicalSourceID(
	scope memory.Scope,
	source memory.SourceRef,
) (string, bool, error) {
	nativeID := memory.TurnSourceID(source.ExportID)

	key, err := canonicalKey(scope, badgerRecordSource, nativeID)
	if err != nil {
		return "", false, err
	}

	var native memory.CanonicalSourceRecord

	readErr := s.db.View(func(txn *badger.Txn) error {
		return getCanonicalRecord(txn, key, badgerRecordSource, &native)
	})
	if readErr == nil {
		return nativeID, true, nil
	}

	if errors.Is(readErr, badger.ErrKeyNotFound) {
		return "", false, nil
	}

	return "", false, canonicalStoreError("resolve canonical source identity", readErr)
}
