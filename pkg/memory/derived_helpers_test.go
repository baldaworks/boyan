package memory

import (
	"testing"
	"time"
)

func derivedTestAtom(t *testing.T, scope Scope) Atom {
	t.Helper()

	itemID, err := AtomItemID(scope, AtomCategoryDecision, "Ship the public memory package")
	if err != nil {
		t.Fatalf("AtomItemID() error = %v", err)
	}
	provenance := Provenance{RawSources: []SourceRef{derivedTestSource(scope, "export-1", "turn-1")}}
	revisionID, err := DerivedRevisionID(
		scope,
		itemID,
		"operation-atom",
		[]string{string(AtomCategoryDecision), "Ship the public memory package", string(CandidateRelationNew)},
		provenance,
		nil,
	)
	if err != nil {
		t.Fatalf("DerivedRevisionID() error = %v", err)
	}
	return Atom{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindAtom,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      1,
			OperationID:   "operation-atom",
			Scope:         scope,
			State:         RevisionStateActive,
			Provenance:    provenance,
			CreatedAt:     derivedTestTime(),
		},
		Category: AtomCategoryDecision,
		Text:     "Ship the public memory package",
		Relation: CandidateRelationNew,
	}
}

func derivedTestScope() Scope {
	return Scope{Key: "telegram:1:0", Kind: ScopeKindPersonal}
}

func derivedTestSource(scope Scope, exportID, sourceTurnID string) SourceRef {
	return SourceRef{
		Scope:          scope,
		ExportID:       exportID,
		ConversationID: "conversation-1",
		SourceTurnID:   sourceTurnID,
	}
}

func derivedTestTime() time.Time {
	return time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)
}

func assertDerivedErrorCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	code, class, ok := ClassifyError(err)
	if !ok || code != want || class != ErrorClassPermanent {
		t.Fatalf("ClassifyError(%v) = %q, %q, %v; want %q, permanent, true", err, code, class, ok, want)
	}
}
