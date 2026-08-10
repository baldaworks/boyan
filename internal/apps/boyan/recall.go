package app

import (
	"context"
	"strings"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// RecallService owns canonical hydration and fail-closed validation around a
// disposable lexical projection.  Projection text is never returned without
// a canonical read for the same exact scope.
type RecallService struct {
	canonical  memory.RecallCanonicalReader
	projection memory.RecallProjection
	now        func() time.Time
}

// NewRecallService constructs bounded canonical recall.  A nil projection is
// valid and selects the bounded canonical-tail fallback.
func NewRecallService(canonical memory.RecallCanonicalReader, projection memory.RecallProjection) (*RecallService, error) {
	if canonical == nil {
		return nil, memory.PermanentError(memory.CodeStoreFailure, "recall canonical reader is required", nil)
	}
	return &RecallService{canonical: canonical, projection: projection, now: time.Now}, nil
}

// Search executes one exact-scope recall request.
func (s *RecallService) Search(ctx context.Context, request memory.RecallRequest) (memory.RecallResponse, error) {
	if s == nil || s.canonical == nil {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeDisabled, "recall service is unavailable", nil)
	}
	if ctx == nil {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeInvalidQuery, "recall context is required", nil)
	}
	normalized, err := memory.NormalizeRecallRequest(request)
	if err != nil {
		return memory.RecallResponse{}, err
	}
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	currentSeq, err := s.canonical.CurrentScopeChangeSeq(ctx, normalized.Scope)
	if err != nil {
		return memory.RecallResponse{}, err
	}
	if currentSeq < normalized.MinScopeChangeSeq {
		return memory.RecallResponse{}, memory.RetryableError(memory.CodeConflict, "recall consistency watermark has not caught up", nil)
	}

	if s.projection == nil {
		records, err := s.canonical.SearchRecallTail(ctx, normalized, recallTailLimit(normalized.Limit))
		if err != nil {
			return memory.RecallResponse{}, err
		}
		return s.responseFromRecords(normalized, records, currentSeq, now)
	}
	hits, err := s.projection.SearchRecall(ctx, normalized)
	if err != nil {
		return memory.RecallResponse{}, err
	}
	if len(hits) > memory.MaxRecallCandidates {
		return memory.RecallResponse{}, memory.PermanentError(memory.CodeLimitExceeded, "recall projection candidate bound exceeded", nil)
	}
	if len(hits) == 0 {
		records, err := s.canonical.SearchRecallTail(ctx, normalized, recallTailLimit(normalized.Limit))
		if err != nil {
			return memory.RecallResponse{}, err
		}
		return s.responseFromRecords(normalized, records, currentSeq, now)
	}
	ids := make([]string, 0, len(hits))
	byID := make(map[string]memory.RecallProjectionHit, len(hits))
	for _, hit := range hits {
		if hit.Scope != normalized.Scope {
			return memory.RecallResponse{}, memory.PermanentError(memory.CodeScopeViolation, "recall projection returned a foreign scope", nil)
		}
		if hit.RevisionID == "" {
			return memory.RecallResponse{}, memory.PermanentError(memory.CodeInvalidDerived, "recall projection returned an empty revision", nil)
		}
		if _, exists := byID[hit.RevisionID]; exists {
			continue
		}
		byID[hit.RevisionID] = hit
		ids = append(ids, hit.RevisionID)
	}
	if len(ids) == 0 {
		return emptyRecallResponse(normalized.Scope, currentSeq), nil
	}
	records, err := s.canonical.LoadRecallRecords(ctx, normalized.Scope, ids)
	if err != nil {
		return memory.RecallResponse{}, err
	}
	return s.responseFromProjection(normalized, records, byID, currentSeq, now)
}

func (s *RecallService) responseFromProjection(request memory.RecallRequest, records []memory.RecallRecord, hits map[string]memory.RecallProjectionHit, currentSeq uint64, now time.Time) (memory.RecallResponse, error) {
	results := make([]memory.RecallReference, 0, request.Limit)
	seen := make(map[string]struct{}, len(records))
	for _, record := range records {
		hit, exists := hits[record.RevisionID]
		if !exists {
			continue
		}
		if _, exists := seen[record.RevisionID]; exists {
			continue
		}
		if err := record.Validate(request, now); err != nil {
			if skipStaleRecallError(err) {
				continue
			}
			return memory.RecallResponse{}, err
		}
		if hit.ScopeChangeSeq != 0 && hit.ScopeChangeSeq > currentSeq {
			return memory.RecallResponse{}, memory.PermanentError(memory.CodeConflict, "recall projection is ahead of canonical state", nil)
		}
		results = append(results, recallReference(record, memory.RankRecall(hit.Score, record.CreatedAt, now)))
		seen[record.RevisionID] = struct{}{}
		if len(results) == request.Limit {
			break
		}
	}
	memory.SortRecallReferences(results)
	return recallResponse(request.Scope, currentSeq, results), nil
}

func (s *RecallService) responseFromRecords(request memory.RecallRequest, records []memory.RecallRecord, currentSeq uint64, now time.Time) (memory.RecallResponse, error) {
	results := make([]memory.RecallReference, 0, request.Limit)
	seen := make(map[string]struct{}, len(records))
	tokens := strings.Fields(strings.ToLower(request.Query))
	for _, record := range records {
		if _, exists := seen[record.RevisionID]; exists {
			continue
		}
		if err := record.Validate(request, now); err != nil {
			if skipStaleRecallError(err) {
				continue
			}
			return memory.RecallResponse{}, err
		}
		matched := 0
		lowerText := strings.ToLower(record.Text)
		for _, token := range tokens {
			if strings.Contains(lowerText, token) {
				matched++
			}
		}
		if matched == 0 {
			continue
		}
		score := float64(matched) / float64(len(tokens))
		results = append(results, recallReference(record, memory.RankRecall(score, record.CreatedAt, now)))
		seen[record.RevisionID] = struct{}{}
		if len(results) == request.Limit {
			break
		}
	}
	memory.SortRecallReferences(results)
	return recallResponse(request.Scope, currentSeq, results), nil
}

func recallReference(record memory.RecallRecord, score memory.RecallScore) memory.RecallReference {
	evidence := append([]memory.EvidenceRef(nil), record.Evidence...)
	if len(evidence) > memory.MaxRecallEvidence {
		evidence = evidence[:memory.MaxRecallEvidence]
	}
	var category *memory.AtomCategory
	if record.Category != nil {
		copyOfCategory := *record.Category
		category = &copyOfCategory
	}
	return memory.RecallReference{
		SchemaVersion:  memory.RecallSchemaVersionV1,
		Trust:          memory.ReferenceTrustUntrusted,
		Scope:          record.Scope,
		ItemID:         record.ItemID,
		RevisionID:     record.RevisionID,
		Revision:       record.Revision,
		Kind:           record.Kind,
		State:          record.State,
		Category:       category,
		MemoryKey:      record.MemoryKey,
		Text:           record.Text,
		CreatedAt:      record.CreatedAt,
		Score:          score.Total,
		Explain:        score,
		Evidence:       evidence,
		ScopeChangeSeq: record.ScopeChangeSeq,
	}
}

func skipStaleRecallError(err error) bool {
	code, _, ok := memory.ClassifyError(err)
	return ok && (code == memory.CodeNotFound || code == memory.CodeForgotten)
}

func recallTailLimit(limit int) uint32 {
	if limit < 1 {
		return 1
	}
	want := limit * 4
	if want < limit {
		want = limit
	}
	if want > memory.MaxRecallCandidates {
		want = memory.MaxRecallCandidates
	}
	return uint32(want)
}

func emptyRecallResponse(scope memory.Scope, sequence uint64) memory.RecallResponse {
	return recallResponse(scope, sequence, []memory.RecallReference{})
}

func recallResponse(scope memory.Scope, sequence uint64, results []memory.RecallReference) memory.RecallResponse {
	return memory.RecallResponse{SchemaVersion: memory.RecallSchemaVersionV1, Trust: memory.ReferenceTrustUntrusted, Scope: scope, ScopeChangeSeq: sequence, Results: results}
}

var _ RecallReader = (*RecallService)(nil)
