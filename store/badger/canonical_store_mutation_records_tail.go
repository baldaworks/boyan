package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalMutationTailRecords(
	txn *badger.Txn,
	scope memory.Scope,
	lifecycle []memory.LifecycleEvent,
	heads []memory.ItemHead,
	delivery []memory.DeliveryOutboxRecord,
) error {
	for _, event := range lifecycle {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordLifecycle, event.EventID, event); err != nil {
			return err
		}
	}

	for _, head := range heads {
		key, err := canonicalKey(scope, badgerRecordHead, head.ItemID)
		if err != nil {
			return err
		}

		if err := putCanonicalRecord(txn, key, badgerRecordHead, head); err != nil {
			return err
		}
	}

	for _, item := range delivery {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordDelivery, item.DeliveryID, item); err != nil {
			return err
		}
	}

	return nil
}
