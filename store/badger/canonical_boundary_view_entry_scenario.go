package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func canonicalBoundaryScenario(scope memory.Scope, record memory.RecallRecord, provenance memory.Provenance, operationID string) (canonicalBoundaryEntryView, error) {
	itemID := record.LegacyItemID
	if itemID == "" {
		var err error
		itemID, err = memory.ScenarioItemID(scope, record.TopicKey)
		if err != nil {
			return canonicalBoundaryEntryView{}, err
		}
	}
	meta := memory.RevisionMeta{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Kind:          memory.DerivedKindScenario,
		ItemID:        itemID,
		Revision:      record.Revision,
		OperationID:   operationID,
		Scope:         scope,
		State:         memory.RevisionStateActive,
		Provenance:    provenance,
		CreatedAt:     record.CreatedAt,
	}
	revisionID := record.LegacyRevisionID
	if revisionID == "" {
		var err error
		revisionID, err = memory.DerivedRevisionID(scope, itemID, operationID, []string{record.TopicKey, record.Title, record.Text}, provenance, nil)
		if err != nil {
			return canonicalBoundaryEntryView{}, err
		}
	}
	meta.RevisionID = revisionID
	meta.Supersedes = cloneBoundarySupersedes(record.LegacySupersedes)
	scenario := memory.Scenario{Meta: meta, TopicKey: record.TopicKey, Title: record.Title, Summary: record.Text}
	if err := scenario.Validate(); err != nil {
		return canonicalBoundaryEntryView{}, err
	}
	return canonicalBoundaryEntryView{
		scenario:  &scenario,
		legacyRef: memory.RevisionRef{ItemID: itemID, RevisionID: revisionID},
	}, nil
}
