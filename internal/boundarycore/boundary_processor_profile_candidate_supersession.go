package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func resolveProfileCandidateParents(
	state profileGroundingState,
	candidate ProfileCandidate,
) (*RevisionRef, []RevisionRef, []RevisionTransition, error) {
	var supersedes *RevisionRef
	transitions := []RevisionTransition(nil)
	parents := append([]RevisionRef(nil), candidate.Atoms...)
	parents = append(parents, candidate.Scenarios...)
	if candidate.Supersedes != nil {
		if state.active == nil || *candidate.Supersedes != *state.active {
			return nil, nil, nil, invalidDerived("profile supersession target is not the active profile revision")
		}
		target := *candidate.Supersedes
		supersedes = &target
		parents = appendUniqueRevisionRef(parents, target)
		transitions = append(transitions, RevisionTransition{
			Ref:  target,
			From: RevisionStateActive,
			To:   RevisionStateSuperseded,
		})
	}
	if state.active != nil && supersedes == nil {
		return nil, nil, nil, invalidDerived("profile candidate would create a second active profile revision")
	}
	if state.active == nil && supersedes != nil {
		return nil, nil, nil, invalidDerived("profile candidate supersedes a non-current profile revision")
	}
	return supersedes, parents, transitions, nil
}
