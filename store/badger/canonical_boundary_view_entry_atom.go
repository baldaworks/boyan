package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func canonicalBoundaryAtom(
	scope memory.Scope,
	record memory.RecallRecord,
	provenance memory.Provenance,
	operationID string,
) (canonicalBoundaryEntryView, error) {
	category := memory.AtomCategoryFact
	if record.Category != nil {
		category = *record.Category
	}

	itemID, err := canonicalBoundaryAtomItemID(scope, record, category)
	if err != nil {
		return canonicalBoundaryEntryView{}, err
	}

	meta := memory.RevisionMeta{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Kind:          memory.DerivedKindAtom,
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

		revisionID, err = memory.DerivedRevisionID(
			scope,
			itemID,
			operationID,
			[]string{string(category), record.Text, string(memory.CandidateRelationNew)},
			provenance,
			nil,
		)
		if err != nil {
			return canonicalBoundaryEntryView{}, err
		}
	}

	meta.RevisionID = revisionID

	atom := memory.Atom{
		Meta:     meta,
		Category: category,
		Text:     record.Text,
		Relation: memory.CandidateRelationNew,
	}
	if err := atom.Validate(); err != nil {
		return canonicalBoundaryEntryView{}, err
	}

	return canonicalBoundaryEntryView{
		atom:      &atom,
		legacyRef: memory.RevisionRef{ItemID: itemID, RevisionID: revisionID},
	}, nil
}

func canonicalBoundaryAtomItemID(
	scope memory.Scope,
	record memory.RecallRecord,
	category memory.AtomCategory,
) (string, error) {
	if record.LegacyItemID != "" {
		return record.LegacyItemID, nil
	}

	return memory.AtomItemID(scope, category, record.Text)
}
