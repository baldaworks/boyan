package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2/search"
)

func recallProjectionHits(searchHits search.DocumentMatchCollection) ([]memory.RecallProjectionHit, error) {
	hits := make([]memory.RecallProjectionHit, 0, len(searchHits))
	for _, hit := range searchHits {
		metadata, err := storedBleveMetadata(hit.Fields)
		if err != nil {
			return nil, err
		}
		hits = append(hits, memory.RecallProjectionHit{
			Scope:          metadata.Scope,
			ItemID:         metadata.ItemID,
			RevisionID:     metadata.RevisionID,
			Revision:       metadata.Revision,
			Score:          hit.Score,
			ScopeChangeSeq: metadata.ScopeChangeSeq,
		})
	}
	return hits, nil
}
