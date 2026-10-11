package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundedProfileRevision(
	boundary Boundary,
	operationID string,
	state profileGroundingState,
	candidate ProfileCandidate,
	supersedes *RevisionRef,
	parents []RevisionRef,
) (Profile, error) {
	if len(parents) > e.config.MaxSourcesPerRevision {
		return Profile{}, limitExceeded("profile provenance exceeds the configured limit")
	}

	summary := normalizeDerivedText(candidate.Summary)
	provenance := Provenance{ParentRevisions: parents}

	revisionID, err := DerivedRevisionID(
		boundary.Scope,
		state.itemID,
		operationID,
		[]string{summary},
		provenance,
		supersedes,
	)
	if err != nil {
		return Profile{}, err
	}

	profile := Profile{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindProfile,
			ItemID:        state.itemID,
			RevisionID:    revisionID,
			Revision:      state.maxRevision + 1,
			OperationID:   operationID,
			Scope:         boundary.Scope,
			State:         RevisionStateActive,
			Provenance:    provenance,
			CreatedAt:     boundary.OccurredAt,
			Supersedes:    supersedes,
		},
		Summary: summary,
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}

	return profile, nil
}
