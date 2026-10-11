package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalViewRevision(
	txn *badger.Txn,
	scope memory.Scope,
	revisionID string,
	view *canonicalRecallView,
) error {
	revisionKey, err := canonicalKey(scope, badgerRecordRevision, revisionID)
	if err != nil {
		return err
	}

	if err := getCanonicalRecord(txn, revisionKey, badgerRecordRevision, &view.revision); err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) {
			return err
		}

		return err
	}

	if err := view.revision.Validate(); err != nil {
		return memory.PermanentError(memory.CodeStoreFailure, "stored canonical revision is invalid", err)
	}

	return nil
}
