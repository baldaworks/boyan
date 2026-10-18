package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putCanonicalMutationTailRecords(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	lifecycle []memory.LifecycleEvent,
	heads []memory.ItemHead,
	delivery []memory.DeliveryOutboxRecord,
) error {
	for _, event := range lifecycle {
		if err := putMutationRecord(ctx, txn, scope, Lifecycle, event.EventID, event); err != nil {
			return err
		}
	}

	for _, head := range heads {
		key, err := privacyKey(scope, Head, head.ItemID)
		if err != nil {
			return err
		}

		if err := putPrivacyRecord(ctx, txn, key, Head, head); err != nil {
			return err
		}
	}

	for _, item := range delivery {
		if err := putMutationRecord(ctx, txn, scope, Delivery, item.DeliveryID, item); err != nil {
			return err
		}
	}

	return nil
}
