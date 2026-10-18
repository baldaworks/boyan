package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func historicalRevisionItemComplete(ctx context.Context,
	txn *Tx,
	scope memory.Scope,
	revision memory.MemoryRevision) (bool,
	error) {
	key, err := privacyKey(scope, Item, revision.ItemID)
	if err != nil {
		return false, err
	}

	var item memory.MemoryItem

	err = getPrivacyRecord(ctx, txn, key, Item, &item)
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
	ctx context.Context, txn *Tx,
	source memory.CanonicalSourceRecord,
	message memory.MessageRecord,
) (bool, error) {
	payload, err := readPrivacySourcePayload(ctx, txn, source)
	if err != nil {
		return false, err
	}

	_, embedded, _ := migratedTraceSourceRecord(payload, source.Scope)
	if embedded {
		return historicalSourceRepresentable(ctx, txn, source)
	}

	if message.Payload.ByteSize > memory.MaxDerivedTurnTextBytes {
		return false, nil
	}

	key, err := payloadRecordKey(message.Payload)
	if err != nil {
		return false, err
	}

	var data []byte

	err = getPrivacyRecord(ctx, txn, key, Payload, &data)
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
