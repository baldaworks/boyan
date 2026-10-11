package memory

import "time"

// DeliveryClaimRequest requests a bounded exact-scope outbox lease batch.
type DeliveryClaimRequest struct {
	Scope      Scope     `json:"scope"`
	LeaseOwner string    `json:"lease_owner"`
	Now        time.Time `json:"now"`
	LeaseUntil time.Time `json:"lease_until"`
	Limit      uint32    `json:"limit"`
}

// Validate verifies one bounded delivery claim request.
func (r DeliveryClaimRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.LeaseOwner) || r.Now.IsZero() || !r.LeaseUntil.After(r.Now) ||
		r.Limit == 0 ||
		r.Limit > maxCanonicalDeliveryClaims {
		return invalidDerived("delivery claim request is invalid")
	}

	return nil
}

// DeliverySettlementRequest finishes a lease after an external delivery
// attempt. Only the current lease owner may settle an active lease.
type DeliverySettlementRequest struct {
	Scope       Scope          `json:"scope"`
	DeliveryID  string         `json:"delivery_id"`
	LeaseOwner  string         `json:"lease_owner"`
	Status      DeliveryStatus `json:"status"`
	CompletedAt time.Time      `json:"completed_at"`
}

// Validate verifies one delivery settlement request.
func (r DeliverySettlementRequest) Validate() error {
	if err := r.Scope.Validate(); err != nil {
		return err
	}

	if !isCanonicalID(r.DeliveryID) || !isCanonicalID(r.LeaseOwner) ||
		r.CompletedAt.IsZero() {
		return invalidDerived("delivery settlement request is invalid")
	}

	if r.Status != DeliveryStatusDelivered && r.Status != DeliveryStatusTerminal {
		return invalidDerived("delivery settlement status is invalid")
	}

	return nil
}
