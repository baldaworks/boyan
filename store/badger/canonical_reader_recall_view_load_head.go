package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalViewHead(txn *badger.Txn, scope memory.Scope, activeOnly bool, view *canonicalRecallView) error {
	headKey, err := canonicalKey(scope, badgerRecordHead, view.item.ItemID)
	if err != nil {
		return err
	}

	var head memory.ItemHead
	if err := getCanonicalRecord(txn, headKey, badgerRecordHead, &head); err != nil {
		return err
	}

	if err := head.Validate(); err != nil || head.ItemID != view.item.ItemID {
		return memory.PermanentError(memory.CodeStoreFailure, "stored canonical item head is invalid", err)
	}

	if head.RevisionID == view.revision.RevisionID {
		return nil
	}

	view.state = memory.RevisionStateSuperseded

	if activeOnly {
		return errCanonicalInactive
	}

	return nil
}
