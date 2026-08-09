package boyan_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/baldaworks/boyan"
)

func TestPublicPackageBuildsPortableTurn(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{
		Key:  "telegram:1:0",
		Kind: boyan.ScopeKindPersonal,
	}
	session := boyan.SessionRef{
		SessionID:      "session-1",
		AgentSessionID: "agent-session-1",
	}

	turn, err := boyan.NewTurn(
		scope,
		session,
		"turn-1",
		time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		"user message",
		"assistant message",
	)
	if err != nil {
		t.Fatalf("boyan.NewTurn() error = %v", err)
	}
	if err := turn.Validate(); err != nil {
		t.Fatalf("Turn.Validate() error = %v", err)
	}
}

func TestPublicDerivedAtomRoundTrip(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	source := boyan.SourceRef{
		Scope:        scope,
		ExportID:     "export-1",
		SessionID:    "session-1",
		SourceTurnID: "turn-1",
	}
	provenance := boyan.Provenance{RawSources: []boyan.SourceRef{source}}
	itemID, err := boyan.AtomItemID(scope, boyan.AtomCategoryFact, "The project uses Go")
	if err != nil {
		t.Fatalf("boyan.AtomItemID() error = %v", err)
	}
	revisionID, err := boyan.DerivedRevisionID(
		scope,
		itemID,
		"operation-1",
		[]string{
			string(boyan.AtomCategoryFact),
			"The project uses Go",
			string(boyan.CandidateRelationNew),
		},
		provenance,
		nil,
	)
	if err != nil {
		t.Fatalf("boyan.DerivedRevisionID() error = %v", err)
	}
	want := boyan.Atom{
		Meta: boyan.RevisionMeta{
			SchemaVersion: boyan.DerivedSchemaVersionV1,
			Kind:          boyan.DerivedKindAtom,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      1,
			OperationID:   "operation-1",
			Scope:         scope,
			State:         boyan.RevisionStateActive,
			Provenance:    provenance,
			CreatedAt:     time.Date(2026, time.August, 3, 0, 0, 0, 0, time.UTC),
		},
		Category: boyan.AtomCategoryFact,
		Text:     "The project uses Go",
		Relation: boyan.CandidateRelationNew,
	}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got boyan.Atom
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("round-tripped Atom.Validate() error = %v", err)
	}
}
