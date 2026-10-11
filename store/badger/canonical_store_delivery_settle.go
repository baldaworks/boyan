package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// SettleDeliveryOutbox marks an active exact-worker lease as delivered or
// terminal. A stale or foreign worker cannot settle another worker's lease.
func (s *CanonicalStore) SettleDeliveryOutbox(ctx context.Context, request memory.DeliverySettlementRequest) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}

	if err := request.Validate(); err != nil {
		return err
	}

	claimKey, err := canonicalKey(request.Scope, badgerRecordDeliveryClaim, request.DeliveryID)
	if err != nil {
		return err
	}

	err = s.db.Update(func(txn *badger.Txn) error {
		var claim memory.DeliveryClaim
		if err := getCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, &claim); err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return memory.PermanentError(memory.CodeNotFound, "canonical delivery claim does not exist", nil)
			}

			return err
		}

		if claim.Status != memory.DeliveryStatusLeased || claim.LeaseOwner != request.LeaseOwner {
			return memory.PermanentError(memory.CodeConflict, "canonical delivery lease is not owned by this worker", nil)
		}

		claim.Status = request.Status
		claim.LeaseOwner = ""
		claim.LeaseUntil = nil

		return putCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, claim)
	})
	if err != nil {
		return canonicalStoreError("settle canonical delivery outbox", err)
	}

	return nil
}
