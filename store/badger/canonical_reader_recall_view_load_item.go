package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalViewItem(txn *badger.Txn, scope memory.Scope, view *canonicalRecallView) error {
	itemKey, err := canonicalKey(scope, badgerRecordItem, view.revision.ItemID)
	if err != nil {
		return err
	}

	if err := getCanonicalRecord(txn, itemKey, badgerRecordItem, &view.item); err != nil {
		return err
	}

	if err := view.item.Validate(); err != nil || view.item.Scope != scope || view.item.ItemID != view.revision.ItemID {
		return memory.PermanentError(memory.CodeStoreFailure, "stored canonical item is invalid", err)
	}

	return nil
}
