package badger

import (
	"sort"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (r *CanonicalReader) loadSourceMessageRecords(scope memory.Scope, sourceID string) (
	[]memory.MessageRecord, error,
) {
	var records []memory.MessageRecord

	err := r.store.db.View(func(txn *badger.Txn) error {
		var err error

		records, err = r.loadTraceMessageRecordsTxn(txn, scope, sourceID)

		return err
	})
	if err != nil {
		return nil, canonicalStoreError("load canonical trace messages", err)
	}

	sort.Slice(records, func(i, j int) bool {
		return messageRoleOrder(records[i].Role) < messageRoleOrder(records[j].Role) ||
			messageRoleOrder(records[i].Role) == messageRoleOrder(records[j].Role) && records[i].MessageID < records[j].MessageID
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
		return messageRoleOrderOther
	}
}

func (r *CanonicalReader) readTraceMessageRecord(txn *badger.Txn, key []byte, record *memory.MessageRecord) error {
	if r.beforeTraceMessageRecordRead != nil {
		if err := r.beforeTraceMessageRecordRead(); err != nil {
			return err
		}
	}

	return getPrivacyPreparationRecord(txn, key, badgerRecordMessage, record)
}

func (r *CanonicalReader) loadTraceMessageRecordsTxn(txn *badger.Txn, scope memory.Scope, sourceID string) (
	[]memory.MessageRecord, error,
) {
	ids, err := loadTraceMessageIDs(txn, scope, sourceID)
	if err != nil {
		return nil, err
	}

	if len(ids) > maxCanonicalTraceSourceMessages {
		return nil, traceMembershipUnavailable()
	}

	records := make([]memory.MessageRecord, 0, len(ids))
	for _, id := range ids {
		key, err := canonicalKey(scope, badgerRecordMessage, id)
		if err != nil {
			return nil, err
		}

		var record memory.MessageRecord
		if err := r.readTraceMessageRecord(txn, key, &record); err != nil {
			return nil, err
		}

		if err := validateTraceMessageMember(record, sourceID, id); err != nil {
			return nil, err
		}

		records = append(records, record)
	}

	return records, nil
}
