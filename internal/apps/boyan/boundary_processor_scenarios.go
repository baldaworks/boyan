package app

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundScenarioCandidates(
	boundary Boundary,
	operationID string,
	snapshot ScopeSnapshot,
	candidates []ScenarioCandidate,
) ([]Scenario, []RevisionTransition, error) {
	activeAtoms := make(map[RevisionRef]struct{})
	for _, atom := range snapshot.Atoms {
		if atom.Meta.State == RevisionStateActive {
			activeAtoms[revisionRef(atom.Meta)] = struct{}{}
		}
	}
	activeScenarios := make(map[RevisionRef]Scenario)
	activeItems := make(map[string]RevisionRef)
	maxRevision := make(map[string]uint64)
	for _, scenario := range snapshot.Scenarios {
		ref := revisionRef(scenario.Meta)
		if scenario.Meta.Revision > maxRevision[scenario.Meta.ItemID] {
			maxRevision[scenario.Meta.ItemID] = scenario.Meta.Revision
		}
		if scenario.Meta.State == RevisionStateActive {
			activeScenarios[ref] = scenario
			if _, exists := activeItems[scenario.Meta.ItemID]; exists {
				return nil, nil, invalidDerived("Store snapshot contains multiple active revisions for one scenario")
			}
			activeItems[scenario.Meta.ItemID] = ref
		}
	}

	createdItems := make(map[string]struct{}, len(candidates))
	transitioned := make(map[RevisionRef]struct{}, len(candidates))
	scenarios := make([]Scenario, 0, len(candidates))
	transitions := make([]RevisionTransition, 0, len(candidates))
	for _, candidate := range candidates {
		if len(candidate.TopicKey) > e.config.MaxDerivedTextBytes ||
			len(candidate.Title) > e.config.MaxDerivedTextBytes ||
			len(candidate.Summary) > e.config.MaxDerivedTextBytes {
			return nil, nil, limitExceeded("scenario candidate text exceeds the configured limit")
		}
		if err := candidate.Validate(); err != nil {
			return nil, nil, err
		}
		if len(candidate.Atoms) > e.config.MaxSourcesPerRevision {
			return nil, nil, limitExceeded("scenario candidate exceeds the configured provenance limit")
		}
		for _, atom := range candidate.Atoms {
			if _, ok := activeAtoms[atom]; !ok {
				return nil, nil, invalidDerived("scenario candidate references a non-active same-scope atom")
			}
		}
		topicKey := normalizeDerivedText(candidate.TopicKey)
		title := normalizeDerivedText(candidate.Title)
		summary := normalizeDerivedText(candidate.Summary)
		itemID, err := ScenarioItemID(boundary.Scope, topicKey)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := createdItems[itemID]; exists {
			return nil, nil, invalidDerived("scenario synthesis contains duplicate topic items")
		}
		createdItems[itemID] = struct{}{}

		var supersedes *RevisionRef
		parents := append([]RevisionRef(nil), candidate.Atoms...)
		if candidate.Supersedes != nil {
			target := *candidate.Supersedes
			if _, ok := activeScenarios[target]; !ok || target.ItemID != itemID {
				return nil, nil, invalidDerived("scenario supersession target is not the active same-topic revision")
			}
			if _, exists := transitioned[target]; exists {
				return nil, nil, invalidDerived("scenario synthesis targets one revision more than once")
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
		if prior, exists := activeItems[itemID]; exists {
			if supersedes == nil || prior != *supersedes {
				return nil, nil, invalidDerived("scenario candidate would create a second active topic revision")
			}
		} else if supersedes != nil {
			return nil, nil, invalidDerived("scenario candidate supersedes a non-current topic revision")
		}
		if len(parents) > e.config.MaxSourcesPerRevision {
			return nil, nil, limitExceeded("scenario provenance exceeds the configured limit")
		}
		provenance := Provenance{ParentRevisions: parents}
		revisionID, err := DerivedRevisionID(
			boundary.Scope,
			itemID,
			operationID,
			[]string{topicKey, title, summary},
			provenance,
			supersedes,
		)
		if err != nil {
			return nil, nil, err
		}
		scenario := Scenario{
			Meta: RevisionMeta{
				SchemaVersion: DerivedSchemaVersionV1,
				Kind:          DerivedKindScenario,
				ItemID:        itemID,
				RevisionID:    revisionID,
				Revision:      maxRevision[itemID] + 1,
				OperationID:   operationID,
				Scope:         boundary.Scope,
				State:         RevisionStateActive,
				Provenance:    provenance,
				CreatedAt:     boundary.OccurredAt,
				Supersedes:    supersedes,
			},
			TopicKey: topicKey,
			Title:    title,
			Summary:  summary,
		}
		if err := scenario.Validate(); err != nil {
			return nil, nil, err
		}
		scenarios = append(scenarios, scenario)
	}
	return scenarios, transitions, nil
}
