package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadCanonicalSourcePayload(txn *badger.Txn, ref memory.PayloadRef) ([]byte, error) {
	payloadKey, err := canonicalKey(payloadScope(), badgerRecordPayload, ref.ID)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if err := getCanonicalRecord(txn, payloadKey, badgerRecordPayload, &payload); err != nil {
		return nil, err
	}
	if !isBadgerPayloadValid(payload, ref) {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "stored canonical source payload is invalid", nil)
	}
	return payload, nil
}
