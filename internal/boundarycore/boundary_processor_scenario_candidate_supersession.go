package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func resolveScenarioCandidateParents(
	itemID string,
	state scenarioGroundingState,
	candidate ScenarioCandidate,
	transitioned map[RevisionRef]struct{},
) (*RevisionRef, []RevisionRef, []RevisionTransition, error) {
	if candidate.Supersedes == nil {
		candidate.Supersedes = structuralScenarioTarget(state, itemID)
	}

	var supersedes *RevisionRef

	parents := append([]RevisionRef(nil), candidate.Atoms...)
	transitions := []RevisionTransition(nil)

	if candidate.Supersedes != nil {
		target := *candidate.Supersedes
		if _, ok := state.activeScenarios[target]; !ok || target.ItemID != itemID {
			return nil, nil, nil, invalidDerived("scenario supersession target is not the active same-topic revision")
		}

		if _, exists := transitioned[target]; exists {
			return nil, nil, nil, invalidDerived("scenario synthesis targets one revision more than once")
		}

		transitioned[target] = struct{}{}
		targetCopy := target
		supersedes = &targetCopy
		parents = appendUniqueRevisionRef(parents, target)
		transitions = append(transitions, RevisionTransition{
			Ref:  target,
			From: RevisionStateActive,
			To:   RevisionStateSuperseded,
		})
	}

	if prior, exists := state.activeItems[itemID]; exists {
		if supersedes == nil || prior != *supersedes {
			return nil, nil, nil, invalidDerived("scenario candidate would create a second active topic revision")
		}
	} else if supersedes != nil {
		return nil, nil, nil, invalidDerived("scenario candidate supersedes a non-current topic revision")
	}

	return supersedes, parents, transitions, nil
}

func structuralScenarioTarget(state scenarioGroundingState, itemID string) *RevisionRef {
	target, exists := state.activeItems[itemID]
	if !exists {
		return nil
	}

	return &target
}
