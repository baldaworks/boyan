package memory

func validateCommitTransitions(r CommitRequest) error {
	transitionSeen, err := validateCommitTransitionSet(r.Transitions)
	if err != nil {
		return err
	}
	wantTransitions, err := validateCommitSupersessionTransitions(r)
	if err != nil {
		return err
	}
	if len(transitionSeen) != len(wantTransitions) {
		return invalidDerived("commit contains a transition unrelated to a superseding revision")
	}
	for ref := range transitionSeen {
		if _, ok := wantTransitions[ref]; !ok {
			return invalidDerived("commit contains a transition unrelated to a superseding revision")
		}
	}
	return nil
}

func validateCommitTransitionSet(transitions []RevisionTransition) (map[RevisionRef]struct{}, error) {
	seen := make(map[RevisionRef]struct{}, len(transitions))
	for _, transition := range transitions {
		if err := transition.Validate(); err != nil {
			return nil, err
		}
		if _, ok := seen[transition.Ref]; ok {
			return nil, invalidDerived("commit contains duplicate revision transition")
		}
		seen[transition.Ref] = struct{}{}
	}
	return seen, nil
}

func validateCommitSupersessionTransitions(r CommitRequest) (map[RevisionRef]struct{}, error) {
	wantTransitions := make(map[RevisionRef]struct{}, len(r.Transitions))
	for _, atom := range r.Atoms {
		if err := validateSupersessionTransition(atom.Meta, r.Transitions); err != nil {
			return nil, err
		}
		if !addSupersessionRef(wantTransitions, atom.Meta) {
			return nil, invalidDerived("commit supersedes one revision more than once")
		}
	}
	for _, scenario := range r.Scenarios {
		if err := validateSupersessionTransition(scenario.Meta, r.Transitions); err != nil {
			return nil, err
		}
		if !addSupersessionRef(wantTransitions, scenario.Meta) {
			return nil, invalidDerived("commit supersedes one revision more than once")
		}
	}
	for _, profile := range r.Profiles {
		if err := validateSupersessionTransition(profile.Meta, r.Transitions); err != nil {
			return nil, err
		}
		if !addSupersessionRef(wantTransitions, profile.Meta) {
			return nil, invalidDerived("commit supersedes one revision more than once")
		}
	}
	return wantTransitions, nil
}
