package memoryread_test

import (
	"context"
	"math"
	"testing"
	"time"

	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallHybridUsesContributionsInsteadOfRawScore(t *testing.T) {
	record := recallTestRecord(t, "semantic", time.Now().UTC().Add(-time.Hour))
	hit := m.RecallProjectionHit{Scope: record.Scope, RevisionID: record.RevisionID,
		Score: 999, Lexical: 0.25, Semantic: 0.125}

	service, err := read.NewRecallService(&recallTestReader{records: []m.RecallRecord{record}, seq: 2},
		recallTestProjection{hits: []m.RecallProjectionHit{hit}})
	if err != nil {
		t.Fatal(err)
	}

	response, err := service.Search(context.Background(),
		m.RecallRequest{Scope: record.Scope, Query: "paraphrase", Limit: 1})
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("search: %+v %v", response, err)
	}

	score := response.Results[0].Explain
	if score.Lexical != 0.25 || score.Semantic != 0.125 || score.Total != 0.375+score.Freshness+1 {
		t.Fatalf("hybrid score: %+v", score)
	}
}

func TestSemanticOnlyCandidatesCannotBypassCanonicalRules(t *testing.T) {
	observed := time.Now().UTC().Add(-time.Hour)
	after := observed.Add(2 * time.Hour)
	before := observed.Add(-time.Hour)
	event := m.MemoryKindEvent
	category := m.AtomCategoryPreference

	tests := []struct {
		name   string
		change func(*m.RecallRecord, *m.RecallRequest)
	}{
		{"kind", func(_ *m.RecallRecord, r *m.RecallRequest) { r.Kind = &event }},
		{"category", func(_ *m.RecallRecord, r *m.RecallRequest) { r.Category = &category }},
		{"source", func(_ *m.RecallRecord, r *m.RecallRequest) { r.SourceID = "other-source" }},
		{"conversation", func(_ *m.RecallRecord, r *m.RecallRequest) { r.ConversationID = "other-conversation" }},
		{"memory key", func(_ *m.RecallRecord, r *m.RecallRequest) { r.MemoryKey = "other-key" }},
		{"as of", func(_ *m.RecallRecord, r *m.RecallRequest) { r.AsOf = &before }},
		{"valid from", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Temporal.ValidFrom = &after }},
		{"valid until", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Temporal.ValidUntil = &observed }},
		{"expiry", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Temporal.ExpiresAt = &observed }},
		{"sensitive", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Sensitivity = m.SensitivitySensitive }},
		{"retention", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Retention = "unsupported" }},
		{"superseded", func(r *m.RecallRecord, _ *m.RecallRequest) { r.State = m.RevisionStateSuperseded }},
		{"forgotten", func(r *m.RecallRecord, _ *m.RecallRequest) { r.State = m.RevisionStateInvalidated }},
		{"foreign scope", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Scope.Key = "foreign" }},
		{"missing canonical identity", func(r *m.RecallRecord, _ *m.RecallRequest) { r.RevisionID = "other-revision" }},
		{"invalid canonical identity", func(r *m.RecallRecord, _ *m.RecallRequest) { r.ItemID = "" }},
		{"foreign provenance", func(r *m.RecallRecord, _ *m.RecallRequest) {
			r.SourceRefs = []m.SourceRef{{Scope: m.Scope{Key: "foreign", Kind: m.ScopeKindPersonal}}}
		}},
		{"ungrounded evidence", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Evidence[0].SourceID = "foreign-source" }},
		{"missing evidence", func(r *m.RecallRecord, _ *m.RecallRequest) { r.Evidence = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := recallTestRecord(t, "semantic", observed)
			request := m.RecallRequest{Scope: record.Scope, Query: "unmatched paraphrase", Limit: 1}
			hit := m.RecallProjectionHit{Scope: record.Scope, RevisionID: record.RevisionID, Semantic: 1.0 / 61}
			test.change(&record, &request)

			service, err := read.NewRecallService(&recallTestReader{records: []m.RecallRecord{record}, seq: 2},
				recallTestProjection{hits: []m.RecallProjectionHit{hit}})
			if err != nil {
				t.Fatal(err)
			}

			response, err := service.Search(context.Background(), request)
			if len(response.Results) != 0 {
				t.Fatalf("canonical exclusion bypassed: %+v %v", response, err)
			}

			if err != nil {
				if _, _, ok := m.ClassifyError(err); !ok {
					t.Fatalf("unsafe error: %v", err)
				}
			}
		})
	}
}

func TestRecallRejectsInvalidHybridContributions(t *testing.T) {
	record := recallTestRecord(t, "semantic", time.Now().UTC().Add(-time.Hour))

	for _, bad := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, lexical := range []bool{false, true} {
			hit := m.RecallProjectionHit{Scope: record.Scope, RevisionID: record.RevisionID, Semantic: bad}
			if lexical {
				hit.Lexical, hit.Semantic = bad, 0
			}

			service, err := read.NewRecallService(&recallTestReader{records: []m.RecallRecord{record}, seq: 2},
				recallTestProjection{hits: []m.RecallProjectionHit{hit}})
			if err != nil {
				t.Fatal(err)
			}

			response, err := service.Search(context.Background(),
				m.RecallRequest{Scope: record.Scope, Query: "memory", Limit: 1})
			if err == nil || len(response.Results) != 0 {
				t.Fatalf("invalid component accepted: %+v %v", response, err)
			}
		}
	}
}

type reportedProjection struct{ recallTestProjection }

func (p reportedProjection) SearchRecallWithReport(context.Context, m.RecallRequest) (
	[]m.RecallProjectionHit, *m.RetrievalReport, error,
) {
	return p.hits, &m.RetrievalReport{Effective: "hybrid", Requested: "hybrid", SemanticCandidates: 1}, nil
}

func TestRecallKeepsRequestLocalReportEvenForEmptyResults(t *testing.T) {
	record := recallTestRecord(t, "semantic", time.Now().UTC().Add(-time.Hour))
	for _, hits := range [][]m.RecallProjectionHit{nil, {{Scope: record.Scope, RevisionID: "missing", Semantic: 0.125}}} {
		service, err := read.NewRecallService(&recallTestReader{seq: 2}, reportedProjection{recallTestProjection{hits: hits}})
		if err != nil {
			t.Fatal(err)
		}

		response, err := service.Search(context.Background(),
			m.RecallRequest{Scope: record.Scope, Query: "paraphrase", Limit: 1})
		if err != nil || response.Retrieval == nil || response.Retrieval.Effective != "hybrid" || len(response.Results) != 0 {
			t.Fatalf("report lost: %+v %v", response, err)
		}
	}
}
