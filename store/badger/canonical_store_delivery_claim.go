package badger

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func (s *CanonicalStore) claimDeliveryOutbox(
	request memory.DeliveryClaimRequest,
	prefix []byte,
) ([]memory.ClaimedDelivery, error) {
	claimed := make([]memory.ClaimedDelivery, 0, request.Limit)
	err := s.db.Update(func(txn *badger.Txn) error {
		options := badger.DefaultIteratorOptions
		options.Prefix = prefix

		iterator := txn.NewIterator(options)
		defer iterator.Close()

		for iterator.Seek(prefix); iterator.ValidForPrefix(prefix) && uint32(len(claimed)) < request.Limit; iterator.Next() {
			delivery, skip, err := claimableDelivery(txn, request, iterator.Item().Key())
			if err != nil {
				return err
			}

			if skip {
				continue
			}

			claimed = append(claimed, delivery)
		}

		return nil
	})

	return claimed, err
}

func claimableDelivery(
	txn *badger.Txn,
	request memory.DeliveryClaimRequest,
	key []byte,
) (memory.ClaimedDelivery, bool, error) {
	var record memory.DeliveryOutboxRecord
	if err := getCanonicalRecord(txn, key, badgerRecordDelivery, &record); err != nil {
		return memory.ClaimedDelivery{}, false, err
	}

	claimKey, claim, err := loadDeliveryClaim(txn, request.Scope, record.DeliveryID)
	if err != nil {
		return memory.ClaimedDelivery{}, false, err
	}

	if !shouldClaimDelivery(claim, request.Now) {
		return memory.ClaimedDelivery{}, true, nil
	}

	claim = leasedDeliveryClaim(request, claim.Attempts, record.DeliveryID)
	if err := putCanonicalRecord(txn, claimKey, badgerRecordDeliveryClaim, claim); err != nil {
		return memory.ClaimedDelivery{}, false, err
	}

	return memory.ClaimedDelivery{Record: record, Claim: claim}, false, nil
}
