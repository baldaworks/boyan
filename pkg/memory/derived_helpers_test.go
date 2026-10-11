package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import (
	"testing"
	"time"
)

func derivedTestAtom(t *testing.T, scope memory.Scope) memory.Atom {
	t.Helper()

	itemID, err := memory.AtomItemID(scope, memory.AtomCategoryDecision, "Ship the public memory package")
	if err != nil {
		t.Fatalf("AtomItemID() error = %v", err)
	}

	provenance := memory.Provenance{RawSources: []memory.SourceRef{derivedTestSource(scope, "export-1", "turn-1")}}

	revisionID, err := memory.DerivedRevisionID(
		scope,
		itemID,
		"operation-atom",
		[]string{
			string(memory.AtomCategoryDecision),
			"Ship the public memory package",
			string(memory.CandidateRelationNew),
		},
		provenance,
		nil,
	)
	if err != nil {
		t.Fatalf("DerivedRevisionID() error = %v", err)
	}

	return memory.Atom{
		Meta: memory.RevisionMeta{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Kind:          memory.DerivedKindAtom,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      1,
			OperationID:   "operation-atom",
			Scope:         scope,
			State:         memory.RevisionStateActive,
			Provenance:    provenance,
			CreatedAt:     derivedTestTime(),
		},
		Category: memory.AtomCategoryDecision,
		Text:     "Ship the public memory package",
		Relation: memory.CandidateRelationNew,
	}
}

func derivedTestScope() memory.Scope {
	return memory.Scope{Key: "telegram:1:0", Kind: memory.ScopeKindPersonal}
}

func derivedTestSource(scope memory.Scope, exportID, sourceTurnID string) memory.SourceRef {
	return memory.SourceRef{
		Scope:          scope,
		ExportID:       exportID,
		ConversationID: "conversation-1",
		SourceTurnID:   sourceTurnID,
	}
}

func derivedTestTime() time.Time {
	return time.Date(2026, time.August, 3, 4, 5, 6, 0, time.UTC)
}

func assertDerivedErrorCode(t *testing.T, err error, want memory.ErrorCode) {
	t.Helper()

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != want || class != memory.ErrorClassPermanent {
		t.Fatalf(
			"ClassifyError(%v) = %q, %q, %v; want %q, permanent, true",
			err,
			code,
			class,
			ok,
			want,
		)
	}
}
