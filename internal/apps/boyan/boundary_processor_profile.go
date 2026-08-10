package app

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundProfileCandidate(
	boundary Boundary,
	operationID string,
	snapshot ScopeSnapshot,
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
	if len(candidate.Atoms)+len(candidate.Scenarios) > e.config.MaxSourcesPerRevision {
		return nil, nil, limitExceeded("profile candidate exceeds the configured provenance limit")
	}
	activeAtoms := make(map[RevisionRef]struct{})
	for _, atom := range snapshot.Atoms {
		if atom.Meta.State == RevisionStateActive {
			activeAtoms[revisionRef(atom.Meta)] = struct{}{}
		}
	}
	activeScenarios := make(map[RevisionRef]struct{})
	for _, scenario := range snapshot.Scenarios {
		if scenario.Meta.State == RevisionStateActive {
			activeScenarios[revisionRef(scenario.Meta)] = struct{}{}
		}
	}
	for _, atom := range candidate.Atoms {
		if _, ok := activeAtoms[atom]; !ok {
			return nil, nil, invalidDerived("profile candidate references a non-active same-scope atom")
		}
	}
	for _, scenario := range candidate.Scenarios {
		if _, ok := activeScenarios[scenario]; !ok {
			return nil, nil, invalidDerived("profile candidate references a non-active same-scope scenario")
		}
	}

	itemID, err := ProfileItemID(boundary.Scope)
	if err != nil {
		return nil, nil, err
	}
	maxRevision := uint64(0)
	var active *RevisionRef
	for _, profile := range snapshot.Profiles {
		if profile.Meta.ItemID != itemID {
			return nil, nil, invalidDerived("Store snapshot contains a foreign logical profile item")
		}
		if profile.Meta.Revision > maxRevision {
			maxRevision = profile.Meta.Revision
		}
		if profile.Meta.State == RevisionStateActive {
			if active != nil {
				return nil, nil, invalidDerived("Store snapshot contains multiple active profile revisions")
			}
			ref := revisionRef(profile.Meta)
			active = &ref
		}
	}

	var supersedes *RevisionRef
	transitions := []RevisionTransition(nil)
	parents := append([]RevisionRef(nil), candidate.Atoms...)
	parents = append(parents, candidate.Scenarios...)
	if candidate.Supersedes != nil {
		if active == nil || *candidate.Supersedes != *active {
			return nil, nil, invalidDerived("profile supersession target is not the active profile revision")
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
	if active != nil && supersedes == nil {
		return nil, nil, invalidDerived("profile candidate would create a second active profile revision")
	}
	if active == nil && supersedes != nil {
		return nil, nil, invalidDerived("profile candidate supersedes a non-current profile revision")
	}
	if len(parents) > e.config.MaxSourcesPerRevision {
		return nil, nil, limitExceeded("profile provenance exceeds the configured limit")
	}
	summary := normalizeDerivedText(candidate.Summary)
	provenance := Provenance{ParentRevisions: parents}
	revisionID, err := DerivedRevisionID(
		boundary.Scope,
		itemID,
		operationID,
		[]string{summary},
		provenance,
		supersedes,
	)
	if err != nil {
		return nil, nil, err
	}
	profile := Profile{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindProfile,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      maxRevision + 1,
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
		return nil, nil, err
	}
	return []Profile{profile}, transitions, nil
}
