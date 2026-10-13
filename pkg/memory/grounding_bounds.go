package memory

import "time"

// GroundingBounds are the effective constraints inherited from semantic parents.
// Observation and descriptive event time remain independent of these bounds.
type GroundingBounds struct {
	ValidFrom  *time.Time
	ValidUntil *time.Time
	ExpiresAt  *time.Time
}

// IntersectGroundingBounds combines only the supplied semantic parents. Absent
// bounds are unrestricted; an empty half-open interval fails closed.
func IntersectGroundingBounds(parents []Temporal) (GroundingBounds, error) {
	var result GroundingBounds
	for _, parent := range parents {
		result.ValidFrom = latestGroundingBound(result.ValidFrom, parent.ValidFrom)
		result.ValidUntil = earliestGroundingBound(result.ValidUntil, parent.ValidUntil)
		result.ExpiresAt = earliestGroundingBound(result.ExpiresAt, parent.ExpiresAt)
	}

	if result.ValidFrom != nil &&
		(result.ValidUntil != nil && !result.ValidFrom.Before(*result.ValidUntil) ||
			result.ExpiresAt != nil && !result.ValidFrom.Before(*result.ExpiresAt)) {
		return GroundingBounds{}, invalidDerived("grounding validity intersection is empty")
	}

	result.ValidFrom = cloneGroundingBound(result.ValidFrom)
	result.ValidUntil = cloneGroundingBound(result.ValidUntil)
	result.ExpiresAt = cloneGroundingBound(result.ExpiresAt)

	return result, nil
}

func latestGroundingBound(current, next *time.Time) *time.Time {
	if current == nil || next != nil && next.After(*current) {
		return next
	}

	return current
}

func earliestGroundingBound(current, next *time.Time) *time.Time {
	if current == nil || next != nil && next.Before(*current) {
		return next
	}

	return current
}

func cloneGroundingBound(bound *time.Time) *time.Time {
	if bound == nil {
		return nil
	}

	value := bound.UTC()

	return &value
}
