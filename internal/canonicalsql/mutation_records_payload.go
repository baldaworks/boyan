package canonicalsql

import (
	"context"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func putCanonicalMutationPayloads(ctx context.Context, txn *Tx, payloads []memory.CanonicalPayload) error {
	for _, payload := range payloads {
		key, err := payloadRecordKey(payload.Ref)
		if err != nil {
			return err
		}

		if err := putMutationImmutableRecord(ctx, txn, key, Payload, payload.Data); err != nil {
			return err
		}
	}

	return nil
}
