package memory_test

import (
	"math"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestRecallResponseRejectsMalformedReference(t *testing.T) {
	t.Parallel()

	for _, test := range malformedRecallReferenceCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response, request := validRecallResponse()
			test.mutate(&response, &request)

			if err := response.Validate(request); err == nil {
				t.Fatal("Validate() error = nil, want malformed-reference rejection")
			}
		})
	}
}

type recallResponseMutationCase struct {
	name   string
	mutate func(*memory.RecallResponse, *memory.RecallRequest)
}

func malformedRecallReferenceCases() []recallResponseMutationCase {
	return []recallResponseMutationCase{
		{name: "missing item identity", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].ItemID = ""
		}},
		{name: "zero revision", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Revision = 0
		}},
		{name: "unsupported kind", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Kind = "unknown"
		}},
		{name: "state without memory key", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].MemoryKey = ""
		}},
		{name: "event with memory key", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Kind = memory.MemoryKindEvent
		}},
		{name: "missing created time", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].CreatedAt = time.Time{}
		}},
		{name: "non-finite score", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Score = math.NaN()
		}},
		{name: "incoherent score", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Score++
		}},
		{name: "invalid evidence", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Evidence[0].EndByte = 0
		}},
		{name: "duplicate evidence", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].Evidence = append(response.Results[0].Evidence, response.Results[0].Evidence[0])
		}},
		{name: "missing result watermark", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].ScopeChangeSeq = 0
		}},
		{name: "result ahead of response", mutate: func(response *memory.RecallResponse, _ *memory.RecallRequest) {
			response.Results[0].ScopeChangeSeq = response.ScopeChangeSeq + 1
		}},
		{
			name: "result below requested minimum",
			mutate: func(response *memory.RecallResponse, request *memory.RecallRequest) {
				request.MinScopeChangeSeq = response.Results[0].ScopeChangeSeq + 1
				response.ScopeChangeSeq = request.MinScopeChangeSeq
			},
		},
	}
}

func TestRecallResponseBindsVerifiableRequestFilters(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*memory.RecallRequest)
	}{
		{name: "kind", mutate: func(request *memory.RecallRequest) {
			kind := memory.MemoryKindEvent
			request.Kind = &kind
		}},
		{name: "category", mutate: func(request *memory.RecallRequest) {
			category := memory.AtomCategoryPreference
			request.Category = &category
		}},
		{name: "memory key", mutate: func(request *memory.RecallRequest) {
			request.MemoryKey = "another-key"
		}},
		{name: "source", mutate: func(request *memory.RecallRequest) {
			request.SourceID = "another-source"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			response, request := validRecallResponse()
			test.mutate(&request)

			if err := response.Validate(request); err == nil {
				t.Fatal("Validate() error = nil, want request-filter mismatch rejection")
			}
		})
	}
}

func validRecallResponse() (memory.RecallResponse, memory.RecallRequest) {
	scope := memory.Scope{Key: "test:recall-validation", Kind: memory.ScopeKindPersonal}
	category := memory.AtomCategoryFact
	response := memory.RecallResponse{
		SchemaVersion:  memory.RecallSchemaVersionV1,
		Trust:          memory.ReferenceTrustUntrusted,
		Scope:          scope,
		ScopeChangeSeq: 7,
		Results: []memory.RecallReference{{
			SchemaVersion: memory.RecallSchemaVersionV1,
			Trust:         memory.ReferenceTrustUntrusted,
			Scope:         scope,
			ItemID:        "item-1",
			RevisionID:    "revision-1",
			Revision:      1,
			Kind:          memory.MemoryKindState,
			State:         memory.RevisionStateActive,
			Category:      &category,
			MemoryKey:     "memory-key-1",
			Text:          "private reference",
			CreatedAt:     time.Date(2026, time.October, 14, 12, 0, 0, 0, time.UTC),
			Score:         2.5,
			Explain: memory.RecallScore{
				Lexical: 1, Freshness: 0.5, ExactScope: 1, Total: 2.5,
			},
			Evidence: []memory.EvidenceRef{{
				SourceID: "source-1", MessageID: "message-1", Role: memory.MessageRoleUser,
				EndByte: 4, AssertionMode: memory.AssertionModeUser,
			}},
			ScopeChangeSeq: 7,
		}},
	}

	return response, memory.RecallRequest{
		SchemaVersion: memory.RecallSchemaVersionV1,
		Scope:         scope,
		Query:         "reference",
		Limit:         1,
	}
}
