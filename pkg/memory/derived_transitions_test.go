package memory

import (
	"strings"
	"testing"
)

func TestProvenanceRejectsForeignDuplicateAndExcessiveReferences(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	foreign := Scope{Key: "telegram:-100:42", Kind: ScopeKindGroup}
	foreignErr := (Provenance{RawSources: []SourceRef{derivedTestSource(foreign, "export-1", "turn-1")}}).Validate(scope)
	assertDerivedErrorCode(t, foreignErr, CodeScopeViolation)

	source := derivedTestSource(scope, "export-1", "turn-1")
	duplicateErr := (Provenance{RawSources: []SourceRef{source, source}}).Validate(scope)
	assertDerivedErrorCode(t, duplicateErr, CodeInvalidDerived)

	parents := make([]RevisionRef, MaxSourcesPerRevision+1)
	for index := range parents {
		parents[index] = RevisionRef{ItemID: "item-" + strings.Repeat("x", index+1), RevisionID: "revision-1"}
	}
	limitErr := (Provenance{ParentRevisions: parents}).Validate(scope)
	assertDerivedErrorCode(t, limitErr, CodeLimitExceeded)
}

func TestDerivedStateTransitionsAreAppendOnly(t *testing.T) {
	t.Parallel()

	allowed := [][2]RevisionState{
		{"", RevisionStateActive},
		{RevisionStateActive, RevisionStateSuperseded},
		{RevisionStateActive, RevisionStateInvalidated},
		{RevisionStateSuperseded, RevisionStateInvalidated},
	}
	for _, transition := range allowed {
		if err := ValidateRevisionStateTransition(transition[0], transition[1]); err != nil {
			t.Fatalf("ValidateRevisionStateTransition(%q, %q) error = %v", transition[0], transition[1], err)
		}
	}
	disallowed := [][2]RevisionState{
		{RevisionStateActive, RevisionStateActive},
		{RevisionStateSuperseded, RevisionStateActive},
		{RevisionStateInvalidated, RevisionStateActive},
	}
	for _, transition := range disallowed {
		assertDerivedErrorCode(t, ValidateRevisionStateTransition(transition[0], transition[1]), CodeInvalidDerived)
	}

	if err := ValidateSourceStateTransition("", SourceStateActive); err != nil {
		t.Fatalf("new source transition error = %v", err)
	}
	if err := ValidateSourceStateTransition(SourceStateActive, SourceStateForgotten); err != nil {
		t.Fatalf("forget source transition error = %v", err)
	}
	assertDerivedErrorCode(t, ValidateSourceStateTransition(SourceStateForgotten, SourceStateActive), CodeInvalidDerived)
}
