package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putCanonicalMutationCoreRecords(
	ctx context.Context, txn *Tx,
	scope memory.Scope,
	sources []memory.CanonicalSourceRecord,
	messages []memory.MessageRecord,
	items []memory.MemoryItem,
) error {
	for _, source := range sources {
		if err := putMutationRecord(ctx, txn, scope, Source, source.SourceID, source); err != nil {
			return err
		}
	}

	for _, message := range messages {
		if err := putMutationRecord(ctx, txn, scope, Message, message.MessageID, message); err != nil {
			return err
		}
	}

	for _, item := range items {
		if err := putMutationRecord(ctx, txn, scope, Item, item.ItemID, item); err != nil {
			return err
		}
	}

	return nil
}
