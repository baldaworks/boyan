package canonicalsql

import (
	"context"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

var _ memory.CanonicalStore = (*Owner)(nil)

// ClaimDeliveryOutbox leases eligible exact-scope records in delivery ID order.
func (o *Owner) ClaimDeliveryOutbox(ctx context.Context,
	request memory.DeliveryClaimRequest) ([]memory.ClaimedDelivery, error) {
	if err := mutationContext(ctx); err != nil {
		return nil, err
	}

	if err := request.Validate(); err != nil {
		return nil, err
	}

	claimed := make([]memory.ClaimedDelivery, 0, request.Limit)

	err := o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		return claimDeliveriesTx(ctx, tx, request, &claimed)
	})
	if err != nil {
		return nil, err
	}

	return claimed, nil
}

func claimDeliveriesTx(
	ctx context.Context,
	tx *Tx,
	request memory.DeliveryClaimRequest,
	claimed *[]memory.ClaimedDelivery,
) error {
	var after *Key

	for uint32(len(*claimed)) < request.Limit {
		pageLimit := min(int(request.Limit)-len(*claimed), memory.MaxCanonicalReadRecords)

		rows, err := tx.Scan(ctx, Range{Family: Delivery, Scope: &request.Scope,
			After: after, Limit: pageLimit})
		if err != nil {
			return err
		}

		for _, row := range rows {
			var record memory.DeliveryOutboxRecord
			if err := Decode(Delivery, row.Envelope, &record); err != nil {
				return err
			}

			delivery, eligible, err := leaseDelivery(ctx, tx, request, record)
			if err != nil {
				return err
			}

			if eligible {
				*claimed = append(*claimed, delivery)
			}

			if uint32(len(*claimed)) == request.Limit {
				return nil
			}
		}

		if len(rows) < pageLimit {
			break
		}

		last := rows[len(rows)-1].Key
		after = &last
	}

	return nil
}

func leaseDelivery(ctx context.Context, tx *Tx, request memory.DeliveryClaimRequest,
	record memory.DeliveryOutboxRecord) (memory.ClaimedDelivery, bool, error) {
	key, err := privacyKey(request.Scope, DeliveryClaim, record.DeliveryID)
	if err != nil {
		return memory.ClaimedDelivery{}, false, err
	}

	claim := memory.DeliveryClaim{DeliveryID: record.DeliveryID, Status: memory.DeliveryStatusPending}
	if err := getPrivacyRecord(ctx, tx, key, DeliveryClaim, &claim); err != nil && !privacyMissing(err) {
		return memory.ClaimedDelivery{}, false, err
	}

	if !shouldClaimDelivery(claim, request.Now) {
		return memory.ClaimedDelivery{}, false, nil
	}

	leaseUntil := request.LeaseUntil

	claim = memory.DeliveryClaim{DeliveryID: record.DeliveryID, Status: memory.DeliveryStatusLeased,
		LeaseOwner: request.LeaseOwner, LeaseUntil: &leaseUntil, Attempts: claim.Attempts + 1}
	if err := putPrivacyRecord(ctx, tx, key, DeliveryClaim, claim); err != nil {
		return memory.ClaimedDelivery{}, false, err
	}

	return memory.ClaimedDelivery{Record: record, Claim: claim}, true, nil
}

func shouldClaimDelivery(claim memory.DeliveryClaim, now time.Time) bool {
	if claim.Status == memory.DeliveryStatusDelivered || claim.Status == memory.DeliveryStatusTerminal {
		return false
	}

	return claim.Status != memory.DeliveryStatusLeased || claim.LeaseUntil == nil || !claim.LeaseUntil.After(now)
}

// SettleDeliveryOutbox requires the current leased status and exact worker.
func (o *Owner) SettleDeliveryOutbox(ctx context.Context, request memory.DeliverySettlementRequest) error {
	if err := mutationContext(ctx); err != nil {
		return err
	}

	if err := request.Validate(); err != nil {
		return err
	}

	key, err := privacyKey(request.Scope, DeliveryClaim, request.DeliveryID)
	if err != nil {
		return err
	}

	return o.Update(ctx, func(ctx context.Context, tx *Tx) error {
		var claim memory.DeliveryClaim
		if err := getPrivacyRecord(ctx, tx, key, DeliveryClaim, &claim); err != nil {
			return err
		}

		if claim.Status != memory.DeliveryStatusLeased || claim.LeaseOwner != request.LeaseOwner {
			return memory.PermanentError(
				memory.CodeConflict,
				"canonical delivery lease is not owned by this worker",
				nil,
			)
		}

		claim.Status, claim.LeaseOwner, claim.LeaseUntil = request.Status, "", nil

		return putPrivacyRecord(ctx, tx, key, DeliveryClaim, claim)
	})
}
