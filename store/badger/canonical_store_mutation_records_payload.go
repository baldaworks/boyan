package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func putCanonicalMutationPayloads(txn *badger.Txn, payloads []memory.CanonicalPayload) error {
	for _, payload := range payloads {
		key, err := payloadRecordKey(payload.Ref)
		if err != nil {
			return err
		}

		if err := putCanonicalImmutableRecord(txn, key, badgerRecordPayload, payload.Data); err != nil {
			return err
		}
	}

	return nil
}
