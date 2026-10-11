package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalMutationCoreRecords(
	txn *badger.Txn,
	scope memory.Scope,
	sources []memory.CanonicalSourceRecord,
	messages []memory.MessageRecord,
	items []memory.MemoryItem,
) error {
	for _, source := range sources {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordSource, source.SourceID, source); err != nil {
			return err
		}
	}

	for _, message := range messages {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordMessage, message.MessageID, message); err != nil {
			return err
		}
	}

	for _, item := range items {
		if err := putBadgerCanonicalRecord(txn, scope, badgerRecordItem, item.ItemID, item); err != nil {
			return err
		}
	}

	return nil
}
