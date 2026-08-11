package bleve

import (
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
)

func scrubDeletionBatch(index bleve.Index, scope memory.Scope, hits []*search.DocumentMatch) (*bleve.Batch, error) {
	batch := index.NewBatch()
	for _, hit := range hits {
		scopeKey, scopeKeyOK := hit.Fields[bleveFieldScopeKey].(string)
		scopeKind, scopeKindOK := hit.Fields[bleveFieldScopeKind].(string)
		if !scopeKeyOK || !scopeKindOK || scopeKey != scope.Key || scopeKind != string(scope.Kind) {
			return nil, memory.PermanentError(memory.CodeScopeViolation, "bleve forget scrub returned a foreign scope", nil)
		}
		batch.Delete(hit.ID)
	}
	return batch, nil
}
