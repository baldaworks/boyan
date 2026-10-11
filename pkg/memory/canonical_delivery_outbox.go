package memory

import "time"

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
