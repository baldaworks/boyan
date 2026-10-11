package memoryread

import memory "github.com/baldaworks/boyan/pkg/memory"

func recallReference(record memory.RecallRecord, score memory.RecallScore) memory.RecallReference {
	evidence := append([]memory.EvidenceRef(nil), record.Evidence...)
	if len(evidence) > memory.MaxRecallEvidence {
		evidence = evidence[:memory.MaxRecallEvidence]
	}

	var category *memory.AtomCategory

	if record.Category != nil {
		copyOfCategory := *record.Category
		category = &copyOfCategory
	}

	return memory.RecallReference{
		SchemaVersion:  memory.RecallSchemaVersionV1,
		Trust:          memory.ReferenceTrustUntrusted,
		Scope:          record.Scope,
		ItemID:         record.ItemID,
		RevisionID:     record.RevisionID,
		Revision:       record.Revision,
		Kind:           record.Kind,
		State:          record.State,
		Category:       category,
		MemoryKey:      record.MemoryKey,
		Text:           record.Text,
		CreatedAt:      record.CreatedAt,
		Score:          score.Total,
		Explain:        score,
		Evidence:       evidence,
		ScopeChangeSeq: record.ScopeChangeSeq,
	}
}
