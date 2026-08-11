package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func canonicalBoundaryProfile(scope memory.Scope, record memory.RecallRecord, provenance memory.Provenance, operationID string) (canonicalBoundaryEntryView, error) {
	itemID := record.LegacyItemID
	if itemID == "" {
		var err error
		itemID, err = memory.ProfileItemID(scope)
		if err != nil {
			return canonicalBoundaryEntryView{}, err
		}
	}
	meta := memory.RevisionMeta{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Kind:          memory.DerivedKindProfile,
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
		revisionID, err = memory.DerivedRevisionID(scope, itemID, operationID, []string{record.Text}, provenance, nil)
		if err != nil {
			return canonicalBoundaryEntryView{}, err
		}
	}
	meta.RevisionID = revisionID
	meta.Supersedes = cloneBoundarySupersedes(record.LegacySupersedes)
	profile := memory.Profile{Meta: meta, Summary: record.Text}
	if err := profile.Validate(); err != nil {
		return canonicalBoundaryEntryView{}, err
	}
	return canonicalBoundaryEntryView{
		profile:   &profile,
		legacyRef: memory.RevisionRef{ItemID: itemID, RevisionID: revisionID},
	}, nil
}
