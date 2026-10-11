package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"strings"
	"testing"
)

func TestProvenanceRejectsForeignDuplicateAndExcessiveReferences(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	foreign := memory.Scope{Key: "telegram:-100:42", Kind: memory.ScopeKindGroup}
	foreignErr := (memory.Provenance{
		RawSources: []memory.SourceRef{
			derivedTestSource(
				foreign,
				"export-1",
				"turn-1",
			),
		},
	}).Validate(
		scope,
	)
	assertDerivedErrorCode(t, foreignErr, memory.CodeScopeViolation)

	source := derivedTestSource(scope, "export-1", "turn-1")
	duplicateErr := (memory.Provenance{RawSources: []memory.SourceRef{source, source}}).Validate(scope)
	assertDerivedErrorCode(t, duplicateErr, memory.CodeInvalidDerived)

	parents := make([]memory.RevisionRef, memory.MaxSourcesPerRevision+1)
	for index := range parents {
		parents[index] = memory.RevisionRef{ItemID: "item-" + strings.Repeat("x", index+1), RevisionID: "revision-1"}
	}

	limitErr := (memory.Provenance{ParentRevisions: parents}).Validate(scope)
	assertDerivedErrorCode(t, limitErr, memory.CodeLimitExceeded)
}

func TestDerivedStateTransitionsAreAppendOnly(t *testing.T) {
	t.Parallel()

	allowed := [][2]memory.RevisionState{
		{"", memory.RevisionStateActive},
		{memory.RevisionStateActive, memory.RevisionStateSuperseded},
		{memory.RevisionStateActive, memory.RevisionStateInvalidated},
		{memory.RevisionStateSuperseded, memory.RevisionStateInvalidated},
	}
	for _, transition := range allowed {
		if err := memory.ValidateRevisionStateTransition(transition[0], transition[1]); err != nil {
			t.Fatalf(
				"ValidateRevisionStateTransition(%q, %q) error = %v",
				transition[0],
				transition[1],
				err,
			)
		}
	}

	disallowed := [][2]memory.RevisionState{
		{memory.RevisionStateActive, memory.RevisionStateActive},
		{memory.RevisionStateSuperseded, memory.RevisionStateActive},
		{memory.RevisionStateInvalidated, memory.RevisionStateActive},
	}
	for _, transition := range disallowed {
		assertDerivedErrorCode(
			t,
			memory.ValidateRevisionStateTransition(
				transition[0],
				transition[1],
			),
			memory.CodeInvalidDerived,
		)
	}

	if err := memory.ValidateSourceStateTransition("", memory.SourceStateActive); err != nil {
		t.Fatalf("new source transition error = %v", err)
	}

	if err := memory.ValidateSourceStateTransition(memory.SourceStateActive, memory.SourceStateForgotten); err != nil {
		t.Fatalf("forget source transition error = %v", err)
	}

	assertDerivedErrorCode(
		t,
		memory.ValidateSourceStateTransition(memory.SourceStateForgotten, memory.SourceStateActive),
		memory.CodeInvalidDerived,
	)
}
