package bleve

import (
	"fmt"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	"github.com/blevesearch/bleve/v2/mapping"
)

func bleveDocument(document memory.RecallProjectionDocument) bleveRecallDocument {
	category := ""
	if document.Category != nil {
		category = string(*document.Category)
	}
	return bleveRecallDocument{
		SchemaVersion:  BleveRecallProjectionSchema,
		ScopeKey:       document.Scope.Key,
		ScopeKind:      string(document.Scope.Kind),
		ItemID:         document.ItemID,
		RevisionID:     document.RevisionID,
		Revision:       fmt.Sprintf("%d", document.Revision),
		Kind:           string(document.Kind),
		Category:       category,
		MemoryKey:      string(document.MemoryKey),
		TextStandard:   document.Text,
		TextRussian:    document.Text,
		TextEnglish:    document.Text,
		CreatedAt:      document.CreatedAt.UTC().Format(time.RFC3339Nano),
		SourceIDs:      append([]string(nil), document.SourceIDs...),
		SessionIDs:     append([]string(nil), document.SessionIDs...),
		ScopeChangeSeq: fmt.Sprintf("%d", document.ScopeChangeSeq),
	}
}

func bleveRecallMapping() mapping.IndexMapping {
	mapping := bleve.NewIndexMapping()
	documentMapping := bleve.NewDocumentStaticMapping()
	for _, field := range []string{
		bleveFieldScopeKey,
		bleveFieldScopeKind,
		bleveFieldItemID,
		bleveFieldRevisionID,
		bleveFieldRevision,
		bleveFieldCategory,
		bleveFieldMemoryKey,
		bleveFieldCreatedAt,
		bleveFieldSourceIDs,
		bleveFieldSessionIDs,
		bleveFieldChangeSeq,
	} {
		documentMapping.AddFieldMappingsAt(field, bleve.NewKeywordFieldMapping())
	}
	standard := bleve.NewTextFieldMapping()
	standard.Store = false
	standard.Analyzer = "standard"
	documentMapping.AddFieldMappingsAt(bleveFieldTextStandard, standard)
	russian := bleve.NewTextFieldMapping()
	russian.Store = false
	russian.Analyzer = "ru"
	documentMapping.AddFieldMappingsAt(bleveFieldTextRussian, russian)
	english := bleve.NewTextFieldMapping()
	english.Store = false
	english.Analyzer = "en"
	documentMapping.AddFieldMappingsAt(bleveFieldTextEnglish, english)
	mapping.DefaultMapping = documentMapping
	return mapping
}

func validBleveGenerationID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "/\\\r\n\t")
}
