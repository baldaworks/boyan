package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

func buildForgetScrubSearch(scope memory.Scope, sourceIDs, revisionIDs []string) *bleve.SearchRequest {
	clauses := make([]query.Query, 0, 2)
	if len(revisionIDs) > 0 {
		revisionQueries := make([]query.Query, 0, len(revisionIDs))
		for _, id := range revisionIDs {
			revisionQueries = append(revisionQueries, scrubKeywordQuery(bleveFieldRevisionID, id))
		}
		clauses = append(clauses, bleve.NewDisjunctionQuery(revisionQueries...))
	}
	if len(sourceIDs) > 0 {
		sourceQueries := make([]query.Query, 0, len(sourceIDs))
		for _, id := range sourceIDs {
			sourceQueries = append(sourceQueries, scrubKeywordQuery(bleveFieldSourceIDs, id))
		}
		clauses = append(clauses, bleve.NewDisjunctionQuery(sourceQueries...))
	}
	identityQuery := clauses[0]
	if len(clauses) == 2 {
		identityQuery = bleve.NewDisjunctionQuery(clauses...)
	}
	scopeQuery := bleve.NewConjunctionQuery(
		identityQuery,
		scrubKeywordQuery(bleveFieldScopeKey, scope.Key),
		scrubKeywordQuery(bleveFieldScopeKind, string(scope.Kind)),
	)
	searchRequest := bleve.NewSearchRequestOptions(scopeQuery, memory.MaxSnapshotItems+1, 0, false)
	searchRequest.Fields = []string{bleveFieldScopeKey, bleveFieldScopeKind, bleveFieldRevisionID}
	return searchRequest
}

func scrubKeywordQuery(field, value string) query.Query {
	term := bleve.NewTermQuery(value)
	term.SetField(field)
	return term
}
