package memory

import "testing"

func TestDerivedValuesValidateStableIdentityAndProvenance(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	atom := derivedTestAtom(t, scope)
	if err := atom.Validate(); err != nil {
		t.Fatalf("Atom.Validate() error = %v", err)
	}

	scenarioItemID, err := ScenarioItemID(scope, "release")
	if err != nil {
		t.Fatalf("ScenarioItemID() error = %v", err)
	}
	scenarioProvenance := Provenance{ParentRevisions: []RevisionRef{{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}}}
	scenarioRevisionID, err := DerivedRevisionID(
		scope,
		scenarioItemID,
		"operation-scenario",
		[]string{"release", "Release", "Ship the public memory package"},
		scenarioProvenance,
		nil,
	)
	if err != nil {
		t.Fatalf("scenario DerivedRevisionID() error = %v", err)
	}
	scenario := Scenario{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindScenario,
			ItemID:        scenarioItemID,
			RevisionID:    scenarioRevisionID,
			Revision:      1,
			OperationID:   "operation-scenario",
			Scope:         scope,
			State:         RevisionStateActive,
			Provenance:    scenarioProvenance,
			CreatedAt:     derivedTestTime(),
		},
		TopicKey: "release",
		Title:    "Release",
		Summary:  "Ship the public memory package",
	}
	if err := scenario.Validate(); err != nil {
		t.Fatalf("Scenario.Validate() error = %v", err)
	}

	profileItemID, err := ProfileItemID(scope)
	if err != nil {
		t.Fatalf("ProfileItemID() error = %v", err)
	}
	profileProvenance := Provenance{ParentRevisions: []RevisionRef{{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}}}
	profileRevisionID, err := DerivedRevisionID(
		scope,
		profileItemID,
		"operation-profile",
		[]string{"Prefers portable public packages"},
		profileProvenance,
		nil,
	)
	if err != nil {
		t.Fatalf("profile DerivedRevisionID() error = %v", err)
	}
	profile := Profile{
		Meta: RevisionMeta{
			SchemaVersion: DerivedSchemaVersionV1,
			Kind:          DerivedKindProfile,
			ItemID:        profileItemID,
			RevisionID:    profileRevisionID,
			Revision:      1,
			OperationID:   "operation-profile",
			Scope:         scope,
			State:         RevisionStateActive,
			Provenance:    profileProvenance,
			CreatedAt:     derivedTestTime(),
		},
		Summary: "Prefers portable public packages",
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("Profile.Validate() error = %v", err)
	}
}

func TestAtomRelationsPreserveCrossItemConflictHistory(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	prior := derivedTestAtom(t, scope)
	priorRef := RevisionRef{ItemID: prior.Meta.ItemID, RevisionID: prior.Meta.RevisionID}

	for _, relation := range []CandidateRelation{CandidateRelationCoexist, CandidateRelationSupersede} {
		t.Run(string(relation), func(t *testing.T) {
			text := "Do not ship the public memory package"
			itemID, err := AtomItemID(scope, AtomCategoryDecision, text)
			if err != nil {
				t.Fatalf("AtomItemID() error = %v", err)
			}
			provenance := Provenance{
				RawSources:      []SourceRef{derivedTestSource(scope, "export-2", "turn-2")},
				ParentRevisions: []RevisionRef{priorRef},
			}
			var supersedes *RevisionRef
			if relation == CandidateRelationSupersede {
				supersedes = &priorRef
			}
			revisionID, err := DerivedRevisionID(
				scope,
				itemID,
				"operation-conflict-"+string(relation),
				[]string{string(AtomCategoryDecision), text, string(relation), priorRef.ItemID, priorRef.RevisionID},
				provenance,
				supersedes,
			)
			if err != nil {
				t.Fatalf("DerivedRevisionID() error = %v", err)
			}
			atom := Atom{
				Meta: RevisionMeta{
					SchemaVersion: DerivedSchemaVersionV1,
					Kind:          DerivedKindAtom,
					ItemID:        itemID,
					RevisionID:    revisionID,
					Revision:      1,
					OperationID:   "operation-conflict-" + string(relation),
					Scope:         scope,
					State:         RevisionStateActive,
					Provenance:    provenance,
					CreatedAt:     derivedTestTime(),
					Supersedes:    supersedes,
				},
				Category:        AtomCategoryDecision,
				Text:            text,
				Relation:        relation,
				RelatedRevision: &priorRef,
			}
			if err := atom.Validate(); err != nil {
				t.Fatalf("Atom.Validate() error = %v", err)
			}
		})
	}
}
