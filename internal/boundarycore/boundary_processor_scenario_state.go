package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

type scenarioGroundingState struct {
	activeAtoms     map[RevisionRef]struct{}
	activeScenarios map[RevisionRef]Scenario
	activeItems     map[string]RevisionRef
	maxRevision     map[string]uint64
}

func loadScenarioGroundingState(snapshot ScopeSnapshot) (scenarioGroundingState, error) {
	state := scenarioGroundingState{
		activeAtoms:     make(map[RevisionRef]struct{}),
		activeScenarios: make(map[RevisionRef]Scenario),
		activeItems:     make(map[string]RevisionRef),
		maxRevision:     make(map[string]uint64),
	}
	for _, atom := range snapshot.Atoms {
		if atom.Meta.State == RevisionStateActive {
			state.activeAtoms[revisionRef(atom.Meta)] = struct{}{}
		}
	}
	for _, scenario := range snapshot.Scenarios {
		ref := revisionRef(scenario.Meta)
		if scenario.Meta.Revision > state.maxRevision[scenario.Meta.ItemID] {
			state.maxRevision[scenario.Meta.ItemID] = scenario.Meta.Revision
		}
		if scenario.Meta.State != RevisionStateActive {
			continue
		}
		state.activeScenarios[ref] = scenario
		if _, exists := state.activeItems[scenario.Meta.ItemID]; exists {
			return scenarioGroundingState{}, invalidDerived("Store snapshot contains multiple active revisions for one scenario")
		}
		state.activeItems[scenario.Meta.ItemID] = ref
	}
	return state, nil
}
