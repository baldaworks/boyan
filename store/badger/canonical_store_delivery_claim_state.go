package badger

import (
	"errors"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func loadDeliveryClaim(txn *badger.Txn, scope memory.Scope, deliveryID string) ([]byte, memory.DeliveryClaim, error) {
	claimKey, err := canonicalKey(scope, badgerRecordDeliveryClaim, deliveryID)
	if err != nil {
		return nil, memory.DeliveryClaim{}, err
	}
	claim := memory.DeliveryClaim{DeliveryID: deliveryID, Status: memory.DeliveryStatusPending}
	err = getCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, &claim)
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		return nil, memory.DeliveryClaim{}, err
	}
	return claimKey, claim, nil
}

func shouldClaimDelivery(claim memory.DeliveryClaim, now time.Time) bool {
	if claim.Status == memory.DeliveryStatusDelivered || claim.Status == memory.DeliveryStatusTerminal {
		return false
	}
	return claim.Status != memory.DeliveryStatusLeased || claim.LeaseUntil == nil || !claim.LeaseUntil.After(now)
}

func leasedDeliveryClaim(request memory.DeliveryClaimRequest, attempts uint32, deliveryID string) memory.DeliveryClaim {
	leaseUntil := request.LeaseUntil
	return memory.DeliveryClaim{
		DeliveryID: deliveryID,
		Status:     memory.DeliveryStatusLeased,
		LeaseOwner: request.LeaseOwner,
		LeaseUntil: &leaseUntil,
		Attempts:   attempts + 1,
	}
}
