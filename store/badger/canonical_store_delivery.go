package badger

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ClaimDeliveryOutbox atomically leases pending or expired exact-scope
// deliveries. Immutable records are never modified while delivery state moves.
func (s *CanonicalStore) ClaimDeliveryOutbox(
	ctx context.Context,
	request memory.DeliveryClaimRequest,
) ([]memory.ClaimedDelivery, error) {
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

	claimed, err := s.claimDeliveryOutbox(request, prefix)
	if err != nil {
		return nil, canonicalStoreError("claim canonical delivery outbox", err)
	}

	return claimed, nil
}
