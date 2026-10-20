//nolint:cyclop,lll,funlen // Real SQL lifecycle scenarios retain explicit error checks and fixture shapes.
package sqlite

import (
	"github.com/baldaworks/boyan/pkg/memory"
)

func applyRecord(id string) memory.RecallRecord {
	return memory.RecallRecord{Scope: lexicalTestScope, ItemID: id, RevisionID: id, Revision: 1,
		Kind: memory.MemoryKindState, State: memory.RevisionStateActive, Text: "searchable needle", CreatedAt: lexicalTestTime,
		Temporal: memory.Temporal{ObservedAt: lexicalTestTime}, Sensitivity: memory.SensitivityStandard,
		Retention: memory.RetentionClassStandard, SourceIDs: []string{"source"}, ConversationIDs: []string{"conversation"}, ScopeChangeSeq: 1}
}
