package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundScenarioCandidates(
	boundary Boundary,
	operationID string,
	snapshot BoundarySnapshot,
	candidates []ScenarioCandidate,
) ([]Scenario, []RevisionTransition, error) {
	state, err := loadScenarioGroundingState(snapshot)
	if err != nil {
		return nil, nil, err
	}

	createdItems := make(map[string]struct{}, len(candidates))
	transitioned := make(map[RevisionRef]struct{}, len(candidates))
	scenarios := make([]Scenario, 0, len(candidates))

	transitions := make([]RevisionTransition, 0, len(candidates))
	for _, candidate := range candidates {
		scenario, candidateTransitions, err := e.groundScenarioCandidate(
			boundary,
			operationID,
			state,
			candidate,
			createdItems,
			transitioned,
		)
		if err != nil {
			return nil, nil, err
		}

		scenarios = append(scenarios, scenario)
		transitions = append(transitions, candidateTransitions...)
	}

	return scenarios, transitions, nil
}
