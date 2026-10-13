package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// A denied head must not become the parent of newly written memory. Compare
// the revision so replay of an old outcome never removes a later replacement.
func eraseRemoveHead(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) error {
	key, err := canonicalKey(scope, badgerRecordHead, revision.ItemID)
	if err != nil {
		return err
	}

	var head memory.ItemHead

	err = getCanonicalRecord(txn, key, badgerRecordHead, &head)
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}

	if err != nil {
		return err
	}

	if err := head.Validate(); err != nil {
		return eraseStoreFailure("invalid scope erase target head", err)
	}

	if head.ItemID != revision.ItemID {
		return eraseStoreFailure("scope erase target head item differs", nil)
	}

	if head.RevisionID != revision.RevisionID {
		return nil
	}

	return txn.Delete(key)
}
