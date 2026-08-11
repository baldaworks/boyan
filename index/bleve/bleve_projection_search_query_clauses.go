package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func buildRecallSearchClauses(request memory.RecallRequest) []query.Query {
	clauses := []query.Query{bleve.NewDisjunctionQuery(buildRecallSearchTextQueries(request.Query)...)}
	for _, filter := range []struct {
		value string
		field string
	}{
		{value: request.Scope.Key, field: bleveFieldScopeKey},
		{value: string(request.Scope.Kind), field: bleveFieldScopeKind},
	} {
		clauses = append(clauses, recallSearchTermQuery(filter.value, filter.field))
	}
	if request.Kind != nil {
		clauses = append(clauses, recallSearchTermQuery(string(*request.Kind), bleveFieldKind))
	}
	if request.Category != nil {
		clauses = append(clauses, recallSearchTermQuery(string(*request.Category), bleveFieldCategory))
	}
	if request.MemoryKey != "" {
		clauses = append(clauses, recallSearchTermQuery(string(request.MemoryKey), bleveFieldMemoryKey))
	}
	if request.SourceID != "" {
		clauses = append(clauses, recallSearchTermQuery(request.SourceID, bleveFieldSourceIDs))
	}
	if request.ConversationID != "" {
		clauses = append(clauses, recallSearchTermQuery(request.ConversationID, bleveFieldConversationIDs))
	}
	return clauses
}

func buildRecallSearchTextQueries(text string) []query.Query {
	textQueries := make([]query.Query, 0, 3)
	for _, field := range []string{bleveFieldTextStandard, bleveFieldTextRussian, bleveFieldTextEnglish} {
		textQuery := bleve.NewMatchQuery(text)
		textQuery.SetField(field)
		textQueries = append(textQueries, textQuery)
	}
	return textQueries
}

func recallSearchTermQuery(value, field string) query.Query {
	term := bleve.NewTermQuery(value)
	term.SetField(field)
	return term
}
