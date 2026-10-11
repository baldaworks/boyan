package badger

import memory "github.com/baldaworks/boyan/pkg/memory"

type recallRecordCompatibility struct {
	text              string
	category          memory.AtomCategory
	legacyKind        *memory.DerivedKind
	legacyItemID      string
	legacyRevisionID  string
	legacyOperationID string
	legacySupersedes  *memory.RevisionRef
	legacyParents     []memory.RevisionRef
	topicKey          string
	title             string
}

func buildRecallRecord(scope memory.Scope, view canonicalRecallView, text []byte) memory.RecallRecord {
	compat := buildRecallRecordCompatibility(scope, view.item.Kind, text)
	category := compat.category

	return memory.RecallRecord{
		Scope:             scope,
		ItemID:            view.item.ItemID,
		RevisionID:        view.revision.RevisionID,
		Revision:          view.revision.Revision,
		Kind:              view.item.Kind,
		Category:          &category,
		MemoryKey:         view.item.MemoryKey,
		Text:              compat.text,
		LegacyKind:        compat.legacyKind,
		LegacyItemID:      compat.legacyItemID,
		LegacyRevisionID:  compat.legacyRevisionID,
		LegacyOperationID: compat.legacyOperationID,
		LegacySupersedes:  compat.legacySupersedes,
		LegacyParents:     compat.legacyParents,
		TopicKey:          compat.topicKey,
		Title:             compat.title,
		State:             memory.RevisionStateActive,
		CreatedAt:         view.revision.Temporal.ObservedAt,
		Temporal:          view.revision.Temporal,
		Sensitivity:       view.revision.Sensitivity,
		Retention:         view.revision.Retention,
		Evidence:          append([]memory.EvidenceRef(nil), view.revision.Evidence...),
	}
}
