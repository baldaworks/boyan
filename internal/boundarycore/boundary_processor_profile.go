package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundProfileCandidate(
	boundary Boundary,
	operationID string,
	snapshot BoundarySnapshot,
	candidate *ProfileCandidate,
) ([]Profile, []RevisionTransition, error) {
	if candidate == nil {
		return nil, nil, nil
	}

	if candidate.Disposition == ProfileDispositionSkip {
		if err := candidate.Validate(); err != nil {
			return nil, nil, err
		}

		return nil, nil, nil
	}

	if len(candidate.Summary) > e.config.MaxDerivedTextBytes {
		return nil, nil, limitExceeded("profile candidate text exceeds the configured limit")
	}

	if err := candidate.Validate(); err != nil {
		return nil, nil, err
	}

	state, err := loadProfileGroundingState(boundary, snapshot)
	if err != nil {
		return nil, nil, err
	}

	profile, transitions, err := e.groundProfileRevision(boundary, operationID, state, *candidate)
	if err != nil {
		return nil, nil, err
	}

	return []Profile{profile}, transitions, nil
}
