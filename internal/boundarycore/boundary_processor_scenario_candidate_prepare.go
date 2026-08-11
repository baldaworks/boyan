package boundarycore

import . "github.com/baldaworks/boyan/pkg/memory"

func (e *BoundaryProcessor) prepareScenarioCandidate(
	boundary Boundary,
	state scenarioGroundingState,
	candidate ScenarioCandidate,
	createdItems map[string]struct{},
) (string, string, string, string, error) {
	if len(candidate.TopicKey) > e.config.MaxDerivedTextBytes ||
		len(candidate.Title) > e.config.MaxDerivedTextBytes ||
		len(candidate.Summary) > e.config.MaxDerivedTextBytes {
		return "", "", "", "", limitExceeded("scenario candidate text exceeds the configured limit")
	}
	if err := candidate.Validate(); err != nil {
		return "", "", "", "", err
	}
	if len(candidate.Atoms) > e.config.MaxSourcesPerRevision {
		return "", "", "", "", limitExceeded("scenario candidate exceeds the configured provenance limit")
	}
	for _, atom := range candidate.Atoms {
		if _, ok := state.activeAtoms[atom]; !ok {
			return "", "", "", "", invalidDerived("scenario candidate references a non-active same-scope atom")
		}
	}

	topicKey := normalizeDerivedText(candidate.TopicKey)
	title := normalizeDerivedText(candidate.Title)
	summary := normalizeDerivedText(candidate.Summary)
	itemID, err := ScenarioItemID(boundary.Scope, topicKey)
	if err != nil {
		return "", "", "", "", err
	}
	if _, exists := createdItems[itemID]; exists {
		return "", "", "", "", invalidDerived("scenario synthesis contains duplicate topic items")
	}
	createdItems[itemID] = struct{}{}
	return itemID, topicKey, title, summary, nil
}
