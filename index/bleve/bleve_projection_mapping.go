package bleve

import (
	"github.com/blevesearch/bleve/v2"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/en"
	_ "github.com/blevesearch/bleve/v2/analysis/lang/ru"
	"github.com/blevesearch/bleve/v2/mapping"
)

func bleveRecallMapping() mapping.IndexMapping {
	indexMapping := bleve.NewIndexMapping()
	documentMapping := bleve.NewDocumentStaticMapping()
	for _, field := range bleveRecallKeywordFields() {
		documentMapping.AddFieldMappingsAt(field, bleve.NewKeywordFieldMapping())
	}
	documentMapping.AddFieldMappingsAt(bleveFieldTextStandard, bleveTextFieldMapping("standard"))
	documentMapping.AddFieldMappingsAt(bleveFieldTextRussian, bleveTextFieldMapping("ru"))
	documentMapping.AddFieldMappingsAt(bleveFieldTextEnglish, bleveTextFieldMapping("en"))
	indexMapping.DefaultMapping = documentMapping
	return indexMapping
}

func bleveRecallKeywordFields() []string {
	return []string{
		bleveFieldScopeKey,
		bleveFieldScopeKind,
		bleveFieldItemID,
		bleveFieldRevisionID,
		bleveFieldRevision,
		bleveFieldCategory,
		bleveFieldMemoryKey,
		bleveFieldCreatedAt,
		bleveFieldSourceIDs,
		bleveFieldConversationIDs,
		bleveFieldChangeSeq,
	}
}

func bleveTextFieldMapping(analyzer string) *mapping.FieldMapping {
	text := bleve.NewTextFieldMapping()
	text.Store = false
	text.Analyzer = analyzer
	return text
}
