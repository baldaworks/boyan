package memory

import "time"

// TemporalEligibleAt reports whether an observed revision is eligible at the
// processing instant. EventAt describes an event and does not constrain recall.
func TemporalEligibleAt(temporal Temporal, at time.Time) bool {
	if at.IsZero() || temporal.ObservedAt.IsZero() || temporal.ObservedAt.After(at) {
		return false
	}

	if temporal.ValidFrom != nil && at.Before(*temporal.ValidFrom) {
		return false
	}

	if temporal.ValidUntil != nil && !at.Before(*temporal.ValidUntil) {
		return false
	}

	return temporal.ExpiresAt == nil || at.Before(*temporal.ExpiresAt)
}
