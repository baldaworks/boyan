package badger

import (
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadSourceMessageRecords(scope memory.Scope, sourceID string) ([]memory.MessageRecord, error) {
	prefix, err := canonicalPrefix(scope, badgerRecordMessage)
	if err != nil {
		return nil, err
	}
	var records []memory.MessageRecord
	if err := r.store.db.View(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix); iterator.Next() {
			var record memory.MessageRecord
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordMessage, &record); err != nil {
				return err
			}
			if record.SourceID != sourceID {
				continue
			}
			if len(records) >= 32 {
				return memory.PermanentError(memory.CodeLimitExceeded, "canonical trace source message bound exceeded", nil)
			}
			if err := record.Validate(); err != nil {
				return err
			}
			records = append(records, record)
		}
		return nil
	}); err != nil {
		return nil, canonicalStoreError("scan canonical trace messages", err)
	}
	sort.Slice(records, func(i, j int) bool {
		return messageRoleOrder(records[i].Role) < messageRoleOrder(records[j].Role) ||
			(messageRoleOrder(records[i].Role) == messageRoleOrder(records[j].Role) && records[i].MessageID < records[j].MessageID)
	})
	return records, nil
}

func messageRoleOrder(role memory.MessageRole) int {
	switch role {
	case memory.MessageRoleUser:
		return 0
	case memory.MessageRoleAssistant:
		return 1
	default:
		return 2
	}
}
