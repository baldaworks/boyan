package memory_test

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallHybridScoreIncludesSemanticContribution(t *testing.T) {
	response, request := validRecallResponse()

	body := []byte(`{"lexical":0.25,"semantic":0.125,"freshness":0.5,"exact_scope":1,"total":1.875}`)
	if err := json.Unmarshal(body, &response.Results[0].Explain); err != nil {
		t.Fatal(err)
	}

	response.Results[0].Score = 1.875
	if err := response.Validate(request); err != nil {
		t.Fatalf("hybrid components rejected: %v", err)
	}
}

func TestLegacyRecallSerializationGolden(t *testing.T) {
	score := m.RankRecall(2, time.Unix(0, 0), time.Unix(0, 0))

	body, err := json.Marshal(score)
	if err != nil || string(body) != `{"lexical":2,"freshness":1,"exact_scope":1,"total":4}` {
		t.Fatalf("legacy explanation changed: %s %v", body, err)
	}

	response := m.RecallResponse{SchemaVersion: m.RecallSchemaVersionV1, Trust: m.ReferenceTrustUntrusted,
		Scope: m.Scope{Key: "test:golden", Kind: m.ScopeKindPersonal}, ScopeChangeSeq: 7, Results: []m.RecallReference{}}

	body, err = json.Marshal(response)

	want := `{"schema_version":"boyan-recall/v1","trust":"untrusted_reference",` +
		`"scope":{"key":"test:golden","kind":"personal"},"scope_change_seq":7,"results":[]}`
	if err != nil || string(body) != want {
		t.Fatalf("legacy response changed: %s %v", body, err)
	}
}

func TestRetrievalReportBoundsAndRedaction(t *testing.T) {
	valid := m.RetrievalReport{Effective: "hybrid", Requested: "hybrid", ModelSpacePrefix: "012345abcdef",
		LexicalCandidates: 100, SemanticCandidates: 100, ScannedChunks: 8192,
		OmittedQueryRunes: 4096, OmittedQueryWindows: 4096}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}

	for _, change := range []func(*m.RetrievalReport){
		func(r *m.RetrievalReport) { r.Effective = "https://endpoint/secret" },
		func(r *m.RetrievalReport) { r.Requested = "query text" },
		func(r *m.RetrievalReport) { r.ModelSpacePrefix = "credential" },
		func(r *m.RetrievalReport) { r.Reason = "provider response body" },
		func(r *m.RetrievalReport) { r.LexicalCandidates++ },
		func(r *m.RetrievalReport) { r.SemanticCandidates++ },
		func(r *m.RetrievalReport) { r.ScannedChunks++ },
		func(r *m.RetrievalReport) { r.OmittedQueryRunes++ },
		func(r *m.RetrievalReport) { r.OmittedQueryWindows++ },
		func(r *m.RetrievalReport) { r.ScannedChunks = -1 },
	} {
		r := valid
		change(&r)

		if err := r.Validate(); err == nil {
			t.Fatalf("unsafe report accepted: %+v", r)
		}
	}

	body, err := json.Marshal(valid)
	if err != nil || string(body) != `{"effective":"hybrid"}` {
		t.Fatalf("private diagnostics exposed: %s %v", body, err)
	}
}

func TestRecallSemanticResponseValidation(t *testing.T) {
	for _, value := range []float64{-1, math.NaN(), math.Inf(1)} {
		response, request := validRecallResponse()

		response.Results[0].Explain.Semantic = value
		if err := response.Validate(request); err == nil {
			t.Fatalf("invalid semantic contribution %v accepted", value)
		}
	}

	response, request := validRecallResponse()

	response.Results[0].Explain.Semantic = 0.125
	if err := response.Validate(request); err == nil {
		t.Fatal("semantic contribution omitted from total accepted")
	}
}

func TestRecallReportRejectsUnsafeReason(t *testing.T) {
	response, request := validRecallResponse()

	body := []byte(`{"retrieval":{"effective":"degraded","reason":"credential or provider body"}}`)
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}

	if err := response.Validate(request); err == nil {
		t.Fatal("unsafe retrieval metadata accepted")
	}
}
