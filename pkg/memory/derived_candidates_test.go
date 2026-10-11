package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"strings"
	"testing"
)

func TestModelCandidatesFailClosed(t *testing.T) {
	t.Parallel()

	ref := memory.RevisionRef{ItemID: "item-1", RevisionID: "revision-1"}

	valid := validModelCandidates(ref)

	for index, candidate := range valid {
		if err := candidate.Validate(); err != nil {
			t.Fatalf("valid candidate %d error = %v", index, err)
		}
	}

	invalid := invalidModelCandidates(ref)

	for index, candidate := range invalid {
		err := candidate.Validate()
		if err == nil {
			t.Fatalf("invalid candidate %d error = nil", index)
		}

		code, _, ok := memory.ClassifyError(err)
		if !ok || (code != memory.CodeInvalidDerived && code != memory.CodeLimitExceeded) {
			t.Fatalf("invalid candidate %d error = %v, code = %q", index, err, code)
		}
	}
}

func TestProfileCandidateSkipIsStrictAndRevisionFree(t *testing.T) {
	t.Parallel()

	skip := memory.ProfileCandidate{Disposition: memory.ProfileDispositionSkip}
	if err := skip.Validate(); err != nil {
		t.Fatalf("skip.Validate() error = %v", err)
	}

	if err := (memory.ProfileCandidate{
		Disposition: memory.ProfileDispositionSkip,
		Summary:     "must not write",
	}).Validate(); err == nil {
		t.Fatal("populated skip candidate validated")
	}
}

func TestDerivedValidationErrorsDoNotEchoContent(t *testing.T) {
	t.Parallel()

	secret := "secret-model-output"

	err := (memory.AtomCandidate{
		Category: memory.AtomCategoryFact,
		Text:     strings.Repeat(secret, memory.MaxDerivedTextBytes),
		Relation: memory.CandidateRelationNew,
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want limit error")
	}

	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error exposed rejected content: %v", err)
	}
}

func validModelCandidates(ref memory.RevisionRef) []interface{ Validate() error } {
	return []interface{ Validate() error }{
		memory.AtomCandidate{
			Category: memory.AtomCategoryFact,
			Text:     "The project uses Go",
			Relation: memory.CandidateRelationNew,
		},
		memory.AtomCandidate{
			Category: memory.AtomCategoryDecision,
			Text:     "Ship it",
			Relation: memory.CandidateRelationSupersede,
			Target:   &ref,
		},
		memory.AtomCandidate{
			Category: memory.AtomCategoryDecision,
			Text:     "Do not ship it",
			Relation: memory.CandidateRelationCoexist,
			Target:   &ref,
		},
		memory.ScenarioCandidate{
			TopicKey: "release",
			Title:    "Release",
			Summary:  "Release context",
			Atoms:    []memory.RevisionRef{ref},
		},
		memory.ProfileCandidate{
			Disposition: memory.ProfileDispositionUpsert,
			Summary:     "Long-lived context",
			Scenarios:   []memory.RevisionRef{ref},
		},
	}
}

func invalidModelCandidates(ref memory.RevisionRef) []interface{ Validate() error } {
	return []interface{ Validate() error }{
		memory.AtomCandidate{
			Category: memory.AtomCategoryFact,
			Text:     "missing target",
			Relation: memory.CandidateRelationSupersede,
		},
		memory.AtomCandidate{
			Category: memory.AtomCategoryFact,
			Text:     "missing target",
			Relation: memory.CandidateRelationCoexist,
		},
		memory.AtomCandidate{
			Category: memory.AtomCategoryFact,
			Text:     "unexpected target",
			Relation: memory.CandidateRelationNew,
			Target:   &ref,
		},
		memory.ScenarioCandidate{TopicKey: "release", Title: "Release", Summary: "missing atoms"},
		memory.ProfileCandidate{Disposition: memory.ProfileDispositionUpsert, Summary: "missing parents"},
		memory.ProfileCandidate{
			Disposition: memory.ProfileDispositionUpsert,
			Summary:     strings.Repeat("x", memory.MaxDerivedTextBytes+1),
			Atoms:       []memory.RevisionRef{ref},
		},
		memory.ProfileCandidate{
			Disposition: memory.ProfileDispositionUpsert,
			Summary:     strings.Repeat(" ", memory.MaxDerivedTextBytes+1),
			Atoms:       []memory.RevisionRef{ref},
		},
	}
}
