package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func isDeniedCanonicalRevision(txn *badger.Txn, scope memory.Scope, revisionID string) (bool, error) {
	deniedKey, err := canonicalKey(scope, badgerRecordDeniedRevision, revisionID)
	if err != nil {
		return false, err
	}
	var denied badgerDeniedRevision
	deniedErr := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedRevision, &denied)
	if deniedErr == nil {
		return true, nil
	}
	if errors.Is(deniedErr, badger.ErrKeyNotFound) {
		return false, nil
	}
	return false, deniedErr
}
