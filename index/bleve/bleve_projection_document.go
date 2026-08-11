package bleve

import (
	"fmt"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func bleveDocument(document memory.RecallProjectionDocument) bleveRecallDocument {
	category := ""
	if document.Category != nil {
		category = string(*document.Category)
	}
	return bleveRecallDocument{
		SchemaVersion:   BleveRecallProjectionSchema,
		ScopeKey:        document.Scope.Key,
		ScopeKind:       string(document.Scope.Kind),
		ItemID:          document.ItemID,
		RevisionID:      document.RevisionID,
		Revision:        fmt.Sprintf("%d", document.Revision),
		Kind:            string(document.Kind),
		Category:        category,
		MemoryKey:       string(document.MemoryKey),
		TextStandard:    document.Text,
		TextRussian:     document.Text,
		TextEnglish:     document.Text,
		CreatedAt:       document.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceIDs:       append([]string(nil), document.SourceIDs...),
		ConversationIDs: append([]string(nil), document.ConversationIDs...),
		ScopeChangeSeq:  fmt.Sprintf("%d", document.ScopeChangeSeq),
	}
}
