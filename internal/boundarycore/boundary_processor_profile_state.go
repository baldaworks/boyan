package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

type profileGroundingState struct {
	itemID          string
	maxRevision     uint64
	active          *RevisionRef
	activeAtoms     map[RevisionRef]struct{}
	activeScenarios map[RevisionRef]struct{}
}

func loadProfileGroundingState(boundary Boundary, snapshot ScopeSnapshot) (profileGroundingState, error) {
	itemID, err := ProfileItemID(boundary.Scope)
	if err != nil {
		return profileGroundingState{}, err
	}

	state := profileGroundingState{
		itemID:          itemID,
		activeAtoms:     make(map[RevisionRef]struct{}),
		activeScenarios: make(map[RevisionRef]struct{}),
	}

	for _, atom := range snapshot.Atoms {
		if atom.Meta.State == RevisionStateActive {
			state.activeAtoms[revisionRef(atom.Meta)] = struct{}{}
		}
	}

	for _, scenario := range snapshot.Scenarios {
		if scenario.Meta.State == RevisionStateActive {
			state.activeScenarios[revisionRef(scenario.Meta)] = struct{}{}
		}
	}

	if err := state.loadProfiles(snapshot.Profiles); err != nil {
		return profileGroundingState{}, err
	}

	return state, nil
}

func (state *profileGroundingState) loadProfiles(profiles []Profile) error {
	for _, profile := range profiles {
		if profile.Meta.ItemID != state.itemID {
			return invalidDerived("Store snapshot contains a foreign logical profile item")
		}

		if profile.Meta.Revision > state.maxRevision {
			state.maxRevision = profile.Meta.Revision
		}

		if profile.Meta.State != RevisionStateActive {
			continue
		}

		if state.active != nil {
			return invalidDerived("Store snapshot contains multiple active profile revisions")
		}

		ref := revisionRef(profile.Meta)
		state.active = &ref
	}

	return nil
}
