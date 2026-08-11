package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func checkDeniedRevision(txn *badger.Txn, scope memory.Scope, revisionID string) error {
	deniedKey, err := canonicalKey(scope, badgerRecordDeniedRevision, revisionID)
	if err != nil {
		return err
	}
	var denied badgerDeniedRevision
	if err := getCanonicalRecord(txn, deniedKey, badgerRecordDeniedRevision, &denied); err == nil {
		return errCanonicalDenied
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return nil
}

func checkDeniedSource(txn *badger.Txn, scope memory.Scope, sourceID string) error {
	deniedSourceKey, err := canonicalKey(scope, badgerRecordDeniedSource, sourceID)
	if err != nil {
		return err
	}
	var deniedSource badgerDeniedSource
	if err := getCanonicalRecord(txn, deniedSourceKey, badgerRecordDeniedSource, &deniedSource); err == nil {
		return errCanonicalDenied
	} else if !errors.Is(err, badger.ErrKeyNotFound) {
		return err
	}
	return nil
}
