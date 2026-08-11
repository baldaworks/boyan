package memory

import (
	"strings"
	"testing"
)

func TestModelCandidatesFailClosed(t *testing.T) {
	t.Parallel()

	ref := RevisionRef{ItemID: "item-1", RevisionID: "revision-1"}
	valid := []interface{ Validate() error }{
		AtomCandidate{Category: AtomCategoryFact, Text: "The project uses Go", Relation: CandidateRelationNew},
		AtomCandidate{Category: AtomCategoryDecision, Text: "Ship it", Relation: CandidateRelationSupersede, Target: &ref},
		AtomCandidate{Category: AtomCategoryDecision, Text: "Do not ship it", Relation: CandidateRelationCoexist, Target: &ref},
		ScenarioCandidate{TopicKey: "release", Title: "Release", Summary: "Release context", Atoms: []RevisionRef{ref}},
		ProfileCandidate{Disposition: ProfileDispositionUpsert, Summary: "Long-lived context", Scenarios: []RevisionRef{ref}},
	}
	for index, candidate := range valid {
		if err := candidate.Validate(); err != nil {
			t.Fatalf("valid candidate %d error = %v", index, err)
		}
	}

	invalid := []interface{ Validate() error }{
		AtomCandidate{Category: AtomCategoryFact, Text: "missing target", Relation: CandidateRelationSupersede},
		AtomCandidate{Category: AtomCategoryFact, Text: "missing target", Relation: CandidateRelationCoexist},
		AtomCandidate{Category: AtomCategoryFact, Text: "unexpected target", Relation: CandidateRelationNew, Target: &ref},
		ScenarioCandidate{TopicKey: "release", Title: "Release", Summary: "missing atoms"},
		ProfileCandidate{Disposition: ProfileDispositionUpsert, Summary: "missing parents"},
		ProfileCandidate{Disposition: ProfileDispositionUpsert, Summary: strings.Repeat("x", MaxDerivedTextBytes+1), Atoms: []RevisionRef{ref}},
		ProfileCandidate{Disposition: ProfileDispositionUpsert, Summary: strings.Repeat(" ", MaxDerivedTextBytes+1), Atoms: []RevisionRef{ref}},
	}
	for index, candidate := range invalid {
		err := candidate.Validate()
		if err == nil {
			t.Fatalf("invalid candidate %d error = nil", index)
		}
		code, _, ok := ClassifyError(err)
		if !ok || (code != CodeInvalidDerived && code != CodeLimitExceeded) {
			t.Fatalf("invalid candidate %d error = %v, code = %q", index, err, code)
		}
	}
}

func TestProfileCandidateSkipIsStrictAndRevisionFree(t *testing.T) {
	t.Parallel()

	skip := ProfileCandidate{Disposition: ProfileDispositionSkip}
	if err := skip.Validate(); err != nil {
		t.Fatalf("skip.Validate() error = %v", err)
	}
	if err := (ProfileCandidate{Disposition: ProfileDispositionSkip, Summary: "must not write"}).Validate(); err == nil {
		t.Fatal("populated skip candidate validated")
	}
}

func TestDerivedValidationErrorsDoNotEchoContent(t *testing.T) {
	t.Parallel()

	secret := "secret-model-output"
	err := (AtomCandidate{
		Category: AtomCategoryFact,
		Text:     strings.Repeat(secret, MaxDerivedTextBytes),
		Relation: CandidateRelationNew,
	}).Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want limit error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("validation error exposed rejected content: %v", err)
	}
}
