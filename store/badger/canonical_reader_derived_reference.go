package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

func canonicalDerivedReference(record memory.RecallRecord, legacyKind memory.DerivedKind, category memory.AtomCategory) (memory.DerivedReference, bool) {
	provenance := memory.Provenance{ParentRevisions: append([]memory.RevisionRef(nil), record.LegacyParents...)}
	if legacyKind == memory.DerivedKindAtom || record.LegacyKind == nil {
		if len(record.SourceRefs) > 0 {
			provenance.RawSources = append(provenance.RawSources, record.SourceRefs...)
		} else {
			for _, evidence := range record.Evidence {
				provenance.RawSources = append(provenance.RawSources, memory.SourceRef{
					Scope:          record.Scope,
					ExportID:       evidence.SourceID,
					ConversationID: firstConversation(record.ConversationIDs),
					SourceTurnID:   evidence.SourceID,
				})
			}
		}
	}
	if err := provenance.Validate(record.Scope); err != nil {
		return memory.DerivedReference{}, false
	}
	itemID, revisionID := record.ItemID, record.RevisionID
	if record.LegacyKind != nil {
		itemID, revisionID = record.LegacyItemID, record.LegacyRevisionID
	}
	ref := memory.DerivedReference{
		SchemaVersion: memory.DerivedSchemaVersionV1,
		Trust:         memory.ReferenceTrustUntrusted,
		Kind:          legacyKind,
		Scope:         record.Scope,
		ItemID:        itemID,
		RevisionID:    revisionID,
		Revision:      record.Revision,
		State:         memory.RevisionStateActive,
		Category:      &category,
		TopicKey:      record.TopicKey,
		Title:         record.Title,
		Text:          record.Text,
		CreatedAt:     record.CreatedAt,
		Provenance:    provenance,
	}
	if legacyKind != memory.DerivedKindAtom {
		ref.Category = nil
	}
	if err := ref.Validate(); err != nil {
		return memory.DerivedReference{}, false
	}
	return ref, true
}

func firstConversation(values []string) string {
	if len(values) == 0 {
		return "conversation-unknown"
	}
	return values[0]
}
