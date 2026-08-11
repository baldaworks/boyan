package memory

import "testing"

func TestDerivedEnumsFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{name: "kind", err: DerivedKind("unknown").Validate()},
		{name: "category", err: AtomCategory("unknown").Validate()},
		{name: "revision state", err: RevisionState("unknown").Validate()},
		{name: "source state", err: SourceState("unknown").Validate()},
		{name: "candidate relation", err: CandidateRelation("unknown").Validate()},
		{name: "operation stage", err: OperationStage("unknown").Validate()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertDerivedErrorCode(t, tt.err, CodeInvalidDerived)
		})
	}
}

func TestDerivedIDsAreStableAndOrderIndependent(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	firstItem, err := AtomItemID(scope, AtomCategoryDecision, "  ship   package ")
	if err != nil {
		t.Fatalf("AtomItemID() error = %v", err)
	}
	secondItem, err := AtomItemID(scope, AtomCategoryDecision, "ship package")
	if err != nil {
		t.Fatalf("AtomItemID() normalized error = %v", err)
	}
	if firstItem != secondItem {
		t.Fatalf("AtomItemID() = %q and %q, want stable normalized identity", firstItem, secondItem)
	}
	const wantItemID = "boyan-derived:v1:atom:d5ac93b869df100fe6213d5a7fa0408940826d8288c34f99d15a11a413572bfc"
	if firstItem != wantItemID {
		t.Fatalf("AtomItemID() = %q, want golden %q", firstItem, wantItemID)
	}

	firstSource := derivedTestSource(scope, "export-1", "turn-1")
	secondSource := derivedTestSource(scope, "export-2", "turn-2")
	firstProvenance := Provenance{RawSources: []SourceRef{firstSource, secondSource}}
	secondProvenance := Provenance{RawSources: []SourceRef{secondSource, firstSource}}
	firstRevision, err := DerivedRevisionID(scope, firstItem, "operation-1", []string{"decision", "ship package"}, firstProvenance, nil)
	if err != nil {
		t.Fatalf("DerivedRevisionID() error = %v", err)
	}
	secondRevision, err := DerivedRevisionID(scope, firstItem, "operation-1", []string{"decision", "ship package"}, secondProvenance, nil)
	if err != nil {
		t.Fatalf("DerivedRevisionID() reordered error = %v", err)
	}
	if firstRevision != secondRevision {
		t.Fatalf("DerivedRevisionID() = %q and %q, want provenance-order independence", firstRevision, secondRevision)
	}
	const wantRevisionID = "boyan-derived:v1:revision:605057b2c42d2f0a635f35cd1755da5da0ebeee61cb73219dded5c8680c5f27e"
	if firstRevision != wantRevisionID {
		t.Fatalf("DerivedRevisionID() = %q, want golden %q", firstRevision, wantRevisionID)
	}
}

func TestProcessingOperationIDUsesDerivationVersion(t *testing.T) {
	t.Parallel()

	legacy, err := ProcessingOperationID(OperationStageAtoms, "export-1")
	if err != nil {
		t.Fatalf("ProcessingOperationID() error = %v", err)
	}
	explicitLegacy, err := ProcessingOperationID(OperationStageAtoms, "export-1", LegacyDerivationRef())
	if err != nil {
		t.Fatalf("ProcessingOperationID() explicit legacy error = %v", err)
	}
	if legacy != explicitLegacy {
		t.Fatalf("legacy operation IDs differ: %q != %q", legacy, explicitLegacy)
	}
	versioned := DerivationRef{Pipeline: "pipeline-current", Policy: "policy-current", Prompt: "prompt-current", Model: "model-current"}
	got, err := ProcessingOperationID(OperationStageAtoms, "export-1", versioned)
	if err != nil {
		t.Fatalf("ProcessingOperationID() versioned error = %v", err)
	}
	if got == legacy {
		t.Fatal("versioned derivation reused the legacy operation ID")
	}
}
