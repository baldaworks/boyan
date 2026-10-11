package memory

import "time"

const maxCanonicalDeliveryClaims = 128

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

// ClaimedDelivery joins immutable delivery data to its newly acquired lease.
type ClaimedDelivery struct {
	Record DeliveryOutboxRecord `json:"record"`
	Claim  DeliveryClaim        `json:"claim"`
}
