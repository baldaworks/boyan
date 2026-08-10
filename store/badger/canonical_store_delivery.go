package badger

import (
	"context"
	"errors"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

// ClaimDeliveryOutbox atomically leases pending or expired exact-scope
// deliveries. Immutable records are never modified while delivery state moves.
func (s *CanonicalStore) ClaimDeliveryOutbox(ctx context.Context, request memory.DeliveryClaimRequest) ([]memory.ClaimedDelivery, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	prefix, err := canonicalPrefix(request.Scope, badgerRecordDelivery)
	if err != nil {
		return nil, err
	}
	claimed := make([]memory.ClaimedDelivery, 0, request.Limit)
	err = s.db.Update(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix
		iterator := txn.NewIterator(options)
		defer iterator.Close()
		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(claimed)) < request.Limit; iterator.Next() {
			var record memory.DeliveryOutboxRecord
			if err := getCanonicalRecord(txn, iterator.Item().Key(), badgerRecordDelivery, &record); err != nil {
				return err
			}
			claimKey, err := canonicalKey(request.Scope, badgerRecordDeliveryClaim, record.DeliveryID)
			if err != nil {
				return err
			}
			claim := memory.DeliveryClaim{DeliveryID: record.DeliveryID, Status: memory.DeliveryStatusPending}
			err = getCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, &claim)
			if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
				return err
			}
			if claim.Status == memory.DeliveryStatusDelivered || claim.Status == memory.DeliveryStatusTerminal ||
				(claim.Status == memory.DeliveryStatusLeased && claim.LeaseUntil != nil && claim.LeaseUntil.After(request.Now)) {
				continue
			}
			leaseUntil := request.LeaseUntil
			claim = memory.DeliveryClaim{
				DeliveryID: record.DeliveryID,
				Status:     memory.DeliveryStatusLeased,
				LeaseOwner: request.LeaseOwner,
				LeaseUntil: &leaseUntil,
				Attempts:   claim.Attempts + 1,
			}
			if err := putCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, claim); err != nil {
				return err
			}
			claimed = append(claimed, memory.ClaimedDelivery{Record: record, Claim: claim})
		}
		return nil
	})
	if err != nil {
		return nil, canonicalStoreError("claim canonical delivery outbox", err)
	}
	return claimed, nil
}

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
