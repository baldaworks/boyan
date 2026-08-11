package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundProfileRevision(
	boundary Boundary,
	operationID string,
	state profileGroundingState,
	candidate ProfileCandidate,
) (Profile, []RevisionTransition, error) {
	if err := e.validateProfileCandidateGrounding(state, candidate); err != nil {
		return Profile{}, nil, err
	}
	supersedes, parents, transitions, err := resolveProfileCandidateParents(state, candidate)
	if err != nil {
		return Profile{}, nil, err
	}
	profile, err := e.groundedProfileRevision(boundary, operationID, state, candidate, supersedes, parents)
	if err != nil {
		return Profile{}, nil, err
	}
	return profile, transitions, nil
}
