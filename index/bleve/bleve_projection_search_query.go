package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
)

func buildRecallSearchRequest(request memory.RecallRequest) *bleve.SearchRequest {
	searchRequest := bleve.NewSearchRequestOptions(
		bleve.NewConjunctionQuery(buildRecallSearchClauses(request)...),
		recallSearchCandidateLimit(request.Limit),
		0,
		false,
	)
	searchRequest.Fields = recallSearchFields()
	return searchRequest
}

func recallSearchCandidateLimit(limit int) int {
	candidateLimit := limit * 4
	if candidateLimit > memory.MaxRecallCandidates {
		candidateLimit = memory.MaxRecallCandidates
	}
	return candidateLimit
}

func recallSearchFields() []string {
	return []string{
		bleveFieldScopeKey,
		bleveFieldScopeKind,
		bleveFieldItemID,
		bleveFieldRevisionID,
		bleveFieldRevision,
		bleveFieldChangeSeq,
	}
}
