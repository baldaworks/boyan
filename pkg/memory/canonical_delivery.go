package memory

import "time"

const maxCanonicalDeliveryClaims = 128

// DeliveryOutboxRecord is the canonical post-mutation side-effect record.
// Its payload is an opaque application-owned reference; side-effect delivery
// state remains outside immutable memory records.
type DeliveryOutboxRecord struct {
	DeliveryID  string     `json:"delivery_id"`
	OperationID string     `json:"operation_id"`
	PayloadRef  PayloadRef `json:"payload_ref"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Validate verifies one immutable delivery outbox record.
func (d DeliveryOutboxRecord) Validate() error {
	if !isCanonicalID(d.DeliveryID) || !isCanonicalID(d.OperationID) || d.CreatedAt.IsZero() {
		return invalidDerived("delivery outbox record is invalid")
	}
	return d.PayloadRef.Validate()
}

// DeliveryStatus captures the mutable delivery lifecycle for an immutable
// outbox record.
type DeliveryStatus string

const (
	DeliveryStatusPending   DeliveryStatus = "pending"
	DeliveryStatusLeased    DeliveryStatus = "leased"
	DeliveryStatusDelivered DeliveryStatus = "delivered"
	DeliveryStatusTerminal  DeliveryStatus = "terminal"
)

// DeliveryClaim is the mutable lease state attached to one immutable outbox
// record. Lease expiry permits deterministic recovery after worker failure.
type DeliveryClaim struct {
	DeliveryID string         `json:"delivery_id"`
	Status     DeliveryStatus `json:"status"`
	LeaseOwner string         `json:"lease_owner,omitempty"`
	LeaseUntil *time.Time     `json:"lease_until,omitempty"`
	Attempts   uint32         `json:"attempts"`
}

// Validate verifies one mutable delivery lease record.
func (c DeliveryClaim) Validate() error {
	if !isCanonicalID(c.DeliveryID) {
		return invalidDerived("delivery claim is invalid")
	}
	switch c.Status {
	case DeliveryStatusPending, DeliveryStatusDelivered, DeliveryStatusTerminal:
		if c.LeaseOwner != "" || c.LeaseUntil != nil {
			return invalidDerived("settled delivery claim has a lease")
		}
	case DeliveryStatusLeased:
		if !isCanonicalID(c.LeaseOwner) || c.LeaseUntil == nil || c.LeaseUntil.IsZero() {
			return invalidDerived("leased delivery claim is invalid")
		}
	default:
		return invalidDerived("delivery claim status is invalid")
	}
	return nil
}

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
	if !isCanonicalID(r.LeaseOwner) || r.Now.IsZero() || !r.LeaseUntil.After(r.Now) || r.Limit == 0 || r.Limit > maxCanonicalDeliveryClaims {
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
	if !isCanonicalID(r.DeliveryID) || !isCanonicalID(r.LeaseOwner) || r.CompletedAt.IsZero() {
		return invalidDerived("delivery settlement request is invalid")
	}
	if r.Status != DeliveryStatusDelivered && r.Status != DeliveryStatusTerminal {
		return invalidDerived("delivery settlement status is invalid")
	}
	return nil
}

// ClaimedDelivery joins immutable delivery data to its newly acquired lease.
type ClaimedDelivery struct {
	Record DeliveryOutboxRecord `json:"record"`
	Claim  DeliveryClaim        `json:"claim"`
}
