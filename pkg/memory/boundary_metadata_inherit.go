package memory

import "time"

// InheritBoundaryMetadata combines actual semantic parents' bounds and policy.
// Callers separately verify direct-parent eligibility at the processing instant.
func InheritBoundaryMetadata(observedAt time.Time, parents []BoundaryParentMetadata) (BoundaryParentMetadata, error) {
	if observedAt.IsZero() || len(parents) == 0 || len(parents) > MaxSourcesPerRevision {
		return BoundaryParentMetadata{}, invalidDerived("boundary inheritance requires bounded parents and observation time")
	}

	result := BoundaryParentMetadata{Sensitivity: SensitivityStandard, Retention: RetentionClassStandard}

	temporal := make([]Temporal, 0, len(parents))
	for _, parent := range parents {
		if err := parent.Temporal.Validate(); err != nil {
			return BoundaryParentMetadata{}, err
		}

		if err := validateSensitivityRetention(parent.Sensitivity, parent.Retention); err != nil {
			return BoundaryParentMetadata{}, err
		}

		temporal = append(temporal, parent.Temporal)
		if parent.Sensitivity == SensitivitySensitive {
			result.Sensitivity = SensitivitySensitive
		}

		if parent.Retention == RetentionClassEphemeral {
			result.Retention = RetentionClassEphemeral
		}
	}

	bounds, err := IntersectGroundingBounds(temporal)
	if err != nil {
		return BoundaryParentMetadata{}, err
	}

	result.Temporal = Temporal{ObservedAt: observedAt.UTC(), ValidFrom: bounds.ValidFrom,
		ValidUntil: bounds.ValidUntil, ExpiresAt: bounds.ExpiresAt}

	return result, nil
}
