package bleve

import (
	"context"
	"fmt"
	"strings"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search/query"
)

// SearchRecall queries only the active generation and returns bounded
// metadata. Canonical hydration and fail-closed validation happen elsewhere.
func (p *BleveRecallProjection) SearchRecall(ctx context.Context, request memory.RecallRequest) ([]memory.RecallProjectionHit, error) {
	if err := canonicalContextError(ctx); err != nil {
		return nil, err
	}
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	index := p.activeByScope[projectionScopeKey(normalized.Scope)]
	if index == nil {
		index = p.active
	}
	if index == nil {
		return []memory.RecallProjectionHit{}, nil
	}
	textQueries := make([]query.Query, 0, 3)
	for _, field := range []string{bleveFieldTextStandard, bleveFieldTextRussian, bleveFieldTextEnglish} {
		textQuery := bleve.NewMatchQuery(normalized.Query)
		textQuery.SetField(field)
		textQueries = append(textQueries, textQuery)
	}
	clauses := []query.Query{bleve.NewDisjunctionQuery(textQueries...)}
	for _, filter := range []struct {
		value string
		field string
	}{
		{value: normalized.Scope.Key, field: bleveFieldScopeKey},
		{value: string(normalized.Scope.Kind), field: bleveFieldScopeKind},
	} {
		term := bleve.NewTermQuery(filter.value)
		term.SetField(filter.field)
		clauses = append(clauses, term)
	}
	if normalized.Kind != nil {
		term := bleve.NewTermQuery(string(*normalized.Kind))
		term.SetField(bleveFieldKind)
		clauses = append(clauses, term)
	}
	if normalized.Category != nil {
		term := bleve.NewTermQuery(string(*normalized.Category))
		term.SetField(bleveFieldCategory)
		clauses = append(clauses, term)
	}
	if normalized.MemoryKey != "" {
		term := bleve.NewTermQuery(string(normalized.MemoryKey))
		term.SetField(bleveFieldMemoryKey)
		clauses = append(clauses, term)
	}
	if normalized.SourceID != "" {
		term := bleve.NewTermQuery(normalized.SourceID)
		term.SetField(bleveFieldSourceIDs)
		clauses = append(clauses, term)
	}
	if normalized.SessionID != "" {
		term := bleve.NewTermQuery(normalized.SessionID)
		term.SetField(bleveFieldSessionIDs)
		clauses = append(clauses, term)
	}
	candidateLimit := normalized.Limit * 4
	if candidateLimit > memory.MaxRecallCandidates {
		candidateLimit = memory.MaxRecallCandidates
	}
	searchRequest := bleve.NewSearchRequestOptions(bleve.NewConjunctionQuery(clauses...), candidateLimit, 0, false)
	searchRequest.Fields = []string{
		bleveFieldScopeKey,
		bleveFieldScopeKind,
		bleveFieldItemID,
		bleveFieldRevisionID,
		bleveFieldRevision,
		bleveFieldChangeSeq,
	}
	result, err := index.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf("search bleve recall projection: %w", err)
	}
	hits := make([]memory.RecallProjectionHit, 0, len(result.Hits))
	for _, hit := range result.Hits {
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

// ScrubCanonicalForget removes projection documents for an already durable
// logical-forget decision.  The active generation is disposable and may be
// updated in place for this bounded maintenance operation; canonical denial
// remains the read gate if the projection is unavailable or stale.
func (p *BleveRecallProjection) ScrubCanonicalForget(ctx context.Context, scope memory.Scope, sourceIDs, revisionIDs []string) error {
	if err := canonicalContextError(ctx); err != nil {
		return err
	}
	if err := scope.Validate(); err != nil {
		return err
	}
	if len(sourceIDs) > memory.MaxSnapshotItems || len(revisionIDs) > memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "bleve forget scrub bound exceeded", nil)
	}
	if len(sourceIDs) == 0 && len(revisionIDs) == 0 {
		return nil
	}
	for _, id := range append(append([]string(nil), sourceIDs...), revisionIDs...) {
		if strings.TrimSpace(id) != id || id == "" {
			return memory.PermanentError(memory.CodeInvalidDerived, "bleve forget scrub identity is invalid", nil)
		}
	}
	if p == nil {
		return memory.PermanentError(memory.CodeDisabled, "bleve projection is unavailable", nil)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	index := p.activeByScope[projectionScopeKey(scope)]
	if index == nil {
		index = p.active
	}
	if index == nil {
		return nil
	}
	clauses := make([]query.Query, 0, 2)
	if len(revisionIDs) > 0 {
		revisionQueries := make([]query.Query, 0, len(revisionIDs))
		for _, id := range revisionIDs {
			term := bleve.NewTermQuery(id)
			term.SetField(bleveFieldRevisionID)
			revisionQueries = append(revisionQueries, term)
		}
		clauses = append(clauses, bleve.NewDisjunctionQuery(revisionQueries...))
	}
	if len(sourceIDs) > 0 {
		sourceQueries := make([]query.Query, 0, len(sourceIDs))
		for _, id := range sourceIDs {
			term := bleve.NewTermQuery(id)
			term.SetField(bleveFieldSourceIDs)
			sourceQueries = append(sourceQueries, term)
		}
		clauses = append(clauses, bleve.NewDisjunctionQuery(sourceQueries...))
	}
	identityQuery := clauses[0]
	if len(clauses) == 2 {
		identityQuery = bleve.NewDisjunctionQuery(clauses...)
	}
	scopeQuery := bleve.NewConjunctionQuery(
		identityQuery,
		keywordQuery(bleveFieldScopeKey, scope.Key),
		keywordQuery(bleveFieldScopeKind, string(scope.Kind)),
	)
	search := bleve.NewSearchRequestOptions(scopeQuery, memory.MaxSnapshotItems+1, 0, false)
	search.Fields = []string{bleveFieldScopeKey, bleveFieldScopeKind, bleveFieldRevisionID}
	result, err := index.SearchInContext(ctx, search)
	if err != nil {
		return fmt.Errorf("search bleve forget scrub candidates: %w", err)
	}
	if len(result.Hits) > memory.MaxSnapshotItems {
		return memory.PermanentError(memory.CodeLimitExceeded, "bleve forget scrub candidate bound exceeded", nil)
	}
	if len(result.Hits) == 0 {
		return nil
	}
	batch := index.NewBatch()
	for _, hit := range result.Hits {
		scopeKey, scopeKeyOK := hit.Fields[bleveFieldScopeKey].(string)
		scopeKind, scopeKindOK := hit.Fields[bleveFieldScopeKind].(string)
		if !scopeKeyOK || !scopeKindOK || scopeKey != scope.Key || scopeKind != string(scope.Kind) {
			return memory.PermanentError(memory.CodeScopeViolation, "bleve forget scrub returned a foreign scope", nil)
		}
		batch.Delete(hit.ID)
	}
	if err := index.Batch(batch); err != nil {
		return fmt.Errorf("commit bleve forget scrub deletion: %w", err)
	}
	return nil
}

func keywordQuery(field, value string) query.Query {
	term := bleve.NewTermQuery(value)
	term.SetField(field)
	return term
}
