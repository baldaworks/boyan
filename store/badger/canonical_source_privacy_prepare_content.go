package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func historicalRevisionItemComplete(txn *badger.Txn, scope memory.Scope, revision memory.MemoryRevision) (bool, error) {
	key, err := canonicalKey(scope, badgerRecordItem, revision.ItemID)
	if err != nil {
		return false, err
	}

	var item memory.MemoryItem

	err = getPrivacyPreparationRecord(txn, key, badgerRecordItem, &item)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if item.Validate() != nil || item.Scope != scope || item.ItemID != revision.ItemID {
		return false, invalidSourcePrivacy()
	}

	return true, nil
}

func historicalMessageContentComplete(
	txn *badger.Txn,
	source memory.CanonicalSourceRecord,
	message memory.MessageRecord,
) (bool, error) {
	payload, err := readPrivacySourcePayload(txn, source)
	if err != nil {
		return false, err
	}

	_, embedded, _ := migratedTraceSourceRecord(payload, source.Scope)
	if embedded {
		return historicalSourceRepresentable(txn, source)
	}

	if message.Payload.ByteSize > memory.MaxDerivedTurnTextBytes {
		return false, nil
	}

	key, err := payloadRecordKey(message.Payload)
	if err != nil {
		return false, err
	}

	var data []byte

	err = getPrivacyPreparationRecord(txn, key, badgerRecordPayload, &data)
	if privacyMissing(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	if (memory.CanonicalPayload{Ref: message.Payload, Data: data}).Validate() != nil {
		return false, invalidSourcePrivacy()
	}

	return true, nil
}
