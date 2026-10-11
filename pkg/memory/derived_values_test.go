package memory_test

import memory "github.com/baldaworks/boyan/pkg/memory"

import "testing"

func TestDerivedValuesValidateStableIdentityAndProvenance(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()

	atom := derivedTestAtom(t, scope)
	if err := atom.Validate(); err != nil {
		t.Fatalf("Atom.Validate() error = %v", err)
	}

	scenario := derivedScenarioForAtom(t, scope, atom)

	assertDerivedProfileForScenario(t, scope, scenario)
}

func TestAtomRelationsPreserveCrossItemConflictHistory(t *testing.T) {
	t.Parallel()

	scope := derivedTestScope()
	prior := derivedTestAtom(t, scope)
	priorRef := memory.RevisionRef{ItemID: prior.Meta.ItemID, RevisionID: prior.Meta.RevisionID}

	for _, relation := range []memory.CandidateRelation{
		memory.CandidateRelationCoexist,
		memory.CandidateRelationSupersede,
	} {
		t.Run(string(relation), func(t *testing.T) {
			assertAtomConflictRelation(t, scope, priorRef, relation)
		})
	}
}

func derivedScenarioForAtom(t *testing.T, scope memory.Scope, atom memory.Atom) memory.Scenario {
	t.Helper()

	scenarioItemID, err := memory.ScenarioItemID(scope, "release")
	if err != nil {
		t.Fatalf("ScenarioItemID() error = %v", err)
	}

	scenarioProvenance := memory.Provenance{
		ParentRevisions: []memory.RevisionRef{{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}},
	}

	scenarioRevisionID, err := memory.DerivedRevisionID(
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

	scenario := memory.Scenario{
		Meta: memory.RevisionMeta{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Kind:          memory.DerivedKindScenario,
			ItemID:        scenarioItemID,
			RevisionID:    scenarioRevisionID,
			Revision:      1,
			OperationID:   "operation-scenario",
			Scope:         scope,
			State:         memory.RevisionStateActive,
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

	return scenario
}

func assertDerivedProfileForScenario(t *testing.T, scope memory.Scope, scenario memory.Scenario) {
	t.Helper()

	profileItemID, err := memory.ProfileItemID(scope)
	if err != nil {
		t.Fatalf("ProfileItemID() error = %v", err)
	}

	profileProvenance := memory.Provenance{
		ParentRevisions: []memory.RevisionRef{{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}},
	}

	profileRevisionID, err := memory.DerivedRevisionID(
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

	profile := memory.Profile{
		Meta: memory.RevisionMeta{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Kind:          memory.DerivedKindProfile,
			ItemID:        profileItemID,
			RevisionID:    profileRevisionID,
			Revision:      1,
			OperationID:   "operation-profile",
			Scope:         scope,
			State:         memory.RevisionStateActive,
			Provenance:    profileProvenance,
			CreatedAt:     derivedTestTime(),
		},
		Summary: "Prefers portable public packages",
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("Profile.Validate() error = %v", err)
	}
}

func assertAtomConflictRelation(
	t *testing.T,
	scope memory.Scope,
	priorRef memory.RevisionRef,
	relation memory.CandidateRelation,
) {
	t.Helper()

	text := "Do not ship the public memory package"

	itemID, err := memory.AtomItemID(scope, memory.AtomCategoryDecision, text)
	if err != nil {
		t.Fatalf("AtomItemID() error = %v", err)
	}

	provenance := memory.Provenance{
		RawSources:      []memory.SourceRef{derivedTestSource(scope, "export-2", "turn-2")},
		ParentRevisions: []memory.RevisionRef{priorRef},
	}

	var supersedes *memory.RevisionRef
	if relation == memory.CandidateRelationSupersede {
		supersedes = &priorRef
	}

	revisionID, err := memory.DerivedRevisionID(
		scope,
		itemID,
		"operation-conflict-"+string(relation),
		[]string{string(memory.AtomCategoryDecision), text, string(relation), priorRef.ItemID, priorRef.RevisionID},
		provenance,
		supersedes,
	)
	if err != nil {
		t.Fatalf("DerivedRevisionID() error = %v", err)
	}

	atom := memory.Atom{
		Meta: memory.RevisionMeta{
			SchemaVersion: memory.DerivedSchemaVersionV1,
			Kind:          memory.DerivedKindAtom,
			ItemID:        itemID,
			RevisionID:    revisionID,
			Revision:      1,
			OperationID:   "operation-conflict-" + string(relation),
			Scope:         scope,
			State:         memory.RevisionStateActive,
			Provenance:    provenance,
			CreatedAt:     derivedTestTime(),
			Supersedes:    supersedes,
		},
		Category:        memory.AtomCategoryDecision,
		Text:            text,
		Relation:        relation,
		RelatedRevision: &priorRef,
	}
	if err := atom.Validate(); err != nil {
		t.Fatalf("Atom.Validate() error = %v", err)
	}
}
