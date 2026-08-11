package canonicalcore

import . "github.com/baldaworks/boyan/pkg/memory"

func (p *CanonicalTurnProcessor) buildCanonicalMutation(turn Turn, derivation DerivationRef, scopeState ScopeState, active []ActiveCanonicalMemory, activeView []ActiveMemoryItem, candidates []SemanticCandidate) (CanonicalMutation, error) {
	operationID, err := CanonicalSemanticOperationID(turn.ExportID, derivation)
	if err != nil {
		return CanonicalMutation{}, err
	}
	mutation := CanonicalMutation{
		SchemaVersion:        CanonicalSchemaVersionV1,
		Scope:                turn.Scope,
		ExpectedScopeVersion: scopeState.Version,
		Operation: OperationRecord{
			OperationID: operationID,
			Fingerprint: mustCanonicalFingerprint(turn.ExportID, derivation),
			CommittedAt: turn.CompletedAt,
		},
	}

	sourceID, err := p.appendCanonicalTurnSource(&mutation, turn)
	if err != nil {
		return CanonicalMutation{}, err
	}
	messageByID, err := p.appendCanonicalTurnMessages(&mutation, sourceID, turn.Messages)
	if err != nil {
		return CanonicalMutation{}, err
	}
	if err := p.appendCanonicalTurnRevisions(&mutation, turn, active, activeView, sourceID, messageByID, candidates); err != nil {
		return CanonicalMutation{}, err
	}

	if err := mutation.Validate(); err != nil {
		return CanonicalMutation{}, err
	}
	return mutation, nil
}
