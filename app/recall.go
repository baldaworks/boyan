package app

import (
	"context"
	"strings"
	"time"

	"github.com/baldaworks/boyan"
)

// RecallService owns canonical hydration and fail-closed validation around a
// disposable lexical projection.  Projection text is never returned without
// a canonical read for the same exact scope.
type RecallService struct {
	canonical  boyan.RecallCanonicalReader
	projection boyan.RecallProjection
	now        func() time.Time
}

// NewRecallService constructs bounded canonical recall.  A nil projection is
// valid and selects the bounded canonical-tail fallback.
func NewRecallService(canonical boyan.RecallCanonicalReader, projection boyan.RecallProjection) (*RecallService, error) {
	if canonical == nil {
		return nil, boyan.PermanentError(boyan.CodeStoreFailure, "recall canonical reader is required", nil)
	}
	return &RecallService{canonical: canonical, projection: projection, now: time.Now}, nil
}

// Search executes one exact-scope recall request.
func (s *RecallService) Search(ctx context.Context, request boyan.RecallRequest) (boyan.RecallResponse, error) {
	if s == nil || s.canonical == nil {
		return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeDisabled, "recall service is unavailable", nil)
	}
	if ctx == nil {
		return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeInvalidQuery, "recall context is required", nil)
	}
	normalized, err := boyan.NormalizeRecallRequest(request)
	if err != nil {
		return boyan.RecallResponse{}, err
	}
	now := time.Now().UTC()
	if s.now != nil {
		now = s.now().UTC()
	}
	currentSeq, err := s.canonical.CurrentScopeChangeSeq(ctx, normalized.Scope)
	if err != nil {
		return boyan.RecallResponse{}, err
	}
	if currentSeq < normalized.MinScopeChangeSeq {
		return boyan.RecallResponse{}, boyan.RetryableError(boyan.CodeConflict, "recall consistency watermark has not caught up", nil)
	}

	if s.projection == nil {
		records, err := s.canonical.SearchRecallTail(ctx, normalized, recallTailLimit(normalized.Limit))
		if err != nil {
			return boyan.RecallResponse{}, err
		}
		return s.responseFromRecords(normalized, records, currentSeq, now)
	}
	hits, err := s.projection.SearchRecall(ctx, normalized)
	if err != nil {
		return boyan.RecallResponse{}, err
	}
	if len(hits) > boyan.MaxRecallCandidates {
		return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeLimitExceeded, "recall projection candidate bound exceeded", nil)
	}
	if len(hits) == 0 {
		records, err := s.canonical.SearchRecallTail(ctx, normalized, recallTailLimit(normalized.Limit))
		if err != nil {
			return boyan.RecallResponse{}, err
		}
		return s.responseFromRecords(normalized, records, currentSeq, now)
	}
	ids := make([]string, 0, len(hits))
	byID := make(map[string]boyan.RecallProjectionHit, len(hits))
	for _, hit := range hits {
		if hit.Scope != normalized.Scope {
			return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeScopeViolation, "recall projection returned a foreign scope", nil)
		}
		if hit.RevisionID == "" {
			return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeInvalidDerived, "recall projection returned an empty revision", nil)
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
		return boyan.RecallResponse{}, err
	}
	return s.responseFromProjection(normalized, records, byID, currentSeq, now)
}

func (s *RecallService) responseFromProjection(request boyan.RecallRequest, records []boyan.RecallRecord, hits map[string]boyan.RecallProjectionHit, currentSeq uint64, now time.Time) (boyan.RecallResponse, error) {
	results := make([]boyan.RecallReference, 0, request.Limit)
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
			return boyan.RecallResponse{}, err
		}
		if hit.ScopeChangeSeq != 0 && hit.ScopeChangeSeq > currentSeq {
			return boyan.RecallResponse{}, boyan.PermanentError(boyan.CodeConflict, "recall projection is ahead of canonical state", nil)
		}
		results = append(results, recallReference(record, boyan.RankRecall(hit.Score, record.CreatedAt, now)))
		seen[record.RevisionID] = struct{}{}
		if len(results) == request.Limit {
			break
		}
	}
	boyan.SortRecallReferences(results)
	return recallResponse(request.Scope, currentSeq, results), nil
}

func (s *RecallService) responseFromRecords(request boyan.RecallRequest, records []boyan.RecallRecord, currentSeq uint64, now time.Time) (boyan.RecallResponse, error) {
	results := make([]boyan.RecallReference, 0, request.Limit)
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
			return boyan.RecallResponse{}, err
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
		results = append(results, recallReference(record, boyan.RankRecall(score, record.CreatedAt, now)))
		seen[record.RevisionID] = struct{}{}
		if len(results) == request.Limit {
			break
		}
	}
	boyan.SortRecallReferences(results)
	return recallResponse(request.Scope, currentSeq, results), nil
}

func recallReference(record boyan.RecallRecord, score boyan.RecallScore) boyan.RecallReference {
	evidence := append([]boyan.EvidenceRef(nil), record.Evidence...)
	if len(evidence) > boyan.MaxRecallEvidence {
		evidence = evidence[:boyan.MaxRecallEvidence]
	}
	var category *boyan.AtomCategory
	if record.Category != nil {
		copyOfCategory := *record.Category
		category = &copyOfCategory
	}
	return boyan.RecallReference{
		SchemaVersion:  boyan.RecallSchemaVersionV1,
		Trust:          boyan.ReferenceTrustUntrusted,
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
	code, _, ok := boyan.ClassifyError(err)
	return ok && (code == boyan.CodeNotFound || code == boyan.CodeForgotten)
}

func recallTailLimit(limit int) uint32 {
	if limit < 1 {
		return 1
	}
	want := limit * 4
	if want < limit {
		want = limit
	}
	if want > boyan.MaxRecallCandidates {
		want = boyan.MaxRecallCandidates
	}
	return uint32(want)
}

func emptyRecallResponse(scope boyan.Scope, sequence uint64) boyan.RecallResponse {
	return recallResponse(scope, sequence, []boyan.RecallReference{})
}

func recallResponse(scope boyan.Scope, sequence uint64, results []boyan.RecallReference) boyan.RecallResponse {
	return boyan.RecallResponse{SchemaVersion: boyan.RecallSchemaVersionV1, Trust: boyan.ReferenceTrustUntrusted, Scope: scope, ScopeChangeSeq: sequence, Results: results}
}

var _ RecallReader = (*RecallService)(nil)
