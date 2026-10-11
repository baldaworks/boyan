package badger

import (
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func validateBadgerMutationParents(
	txn *badger.Txn,
	scope memory.Scope,
	revisions []memory.MemoryRevision,
	staged map[string]memory.MemoryRevision,
) error {
	for _, revision := range revisions {
		for _, parentID := range revision.Parents {
			if _, exists := staged[parentID]; exists {
				continue
			}

			parentKey, err := canonicalKey(scope, badgerRecordRevision, parentID)
			if err != nil {
				return err
			}

			if _, err := txn.Get(parentKey); errors.Is(err, badger.ErrKeyNotFound) {
				return memory.PermanentError(memory.CodeConflict, "canonical provenance parent does not exist", nil)
			} else if err != nil {
				return err
			}
		}
	}

	return nil
}
