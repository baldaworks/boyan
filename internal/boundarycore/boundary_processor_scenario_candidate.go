package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) groundScenarioCandidate(
	boundary Boundary,
	operationID string,
	state scenarioGroundingState,
	candidate ScenarioCandidate,
	createdItems map[string]struct{},
	transitioned map[RevisionRef]struct{},
) (Scenario, []RevisionTransition, error) {
	itemID, topicKey, title, summary, err := e.prepareScenarioCandidate(boundary, state, candidate, createdItems)
	if err != nil {
		return Scenario{}, nil, err
	}
	supersedes, parents, transitions, err := resolveScenarioCandidateParents(
		itemID,
		state,
		candidate,
		transitioned,
	)
	if err != nil {
		return Scenario{}, nil, err
	}
	if len(parents) > e.config.MaxSourcesPerRevision {
		return Scenario{}, nil, limitExceeded("scenario provenance exceeds the configured limit")
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
		return Scenario{}, nil, err
	}
	scenario := Scenario{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindScenario,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      state.maxRevision[itemID] + 1,
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
		return Scenario{}, nil, err
	}
	return scenario, transitions, nil
}
