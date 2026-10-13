package boundarycore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/boundarycore"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type temporalStore struct {
	scope    m.Scope
	version  uint64
	loads    []time.Time
	commits  []m.CommitRequest
	outcomes map[string]m.OperationOutcome
}

func (s *temporalStore) LookupOperation(_ context.Context, lookup m.OperationLookup) (m.OperationLookupResult, error) {
	outcome, found := s.outcomes[lookup.OperationID]

	return m.OperationLookupResult{Found: found, Outcome: outcome}, nil
}

func (s *temporalStore) LoadScope(context.Context, m.Scope) (m.ScopeSnapshot, error) {
	return m.ScopeSnapshot{SchemaVersion: m.DerivedSchemaVersionV1, Scope: s.scope, Version: s.version}, nil
}

func (s *temporalStore) LoadBoundarySnapshot(_ context.Context, _ m.Scope, at time.Time) (m.BoundarySnapshot, error) {
	s.loads = append(s.loads, at)
	structural := m.ScopeSnapshot{SchemaVersion: m.DerivedSchemaVersionV1, Scope: s.scope, Version: s.version}
	eligible := m.ScopeView{SchemaVersion: m.DerivedSchemaVersionV1, Scope: s.scope, Version: s.version}

	return m.BoundarySnapshot{Structural: structural, Eligible: eligible}, nil
}

func (s *temporalStore) Commit(_ context.Context, request m.CommitRequest) (m.OperationOutcome, error) {
	s.commits = append(s.commits, request)
	s.version++
	outcome := m.OperationOutcome{SchemaVersion: m.DerivedSchemaVersionV1, OperationID: request.OperationID,
		Stage: request.Stage, Scope: s.scope, ScopeVersion: s.version}
	s.outcomes[request.OperationID] = outcome

	return outcome, nil
}

type temporalModel struct {
	failProfile                 bool
	scenarioCalls, profileCalls int
}

func (s *temporalModel) SynthesizeScenarios(context.Context,
	m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate,
	error) {
	s.scenarioCalls++

	return nil, nil
}

func (s *temporalModel) SynthesizeProfile(context.Context, m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	s.profileCalls++
	if s.failProfile {
		return nil, m.PermanentError(m.CodeInvalidDerived, "invalid profile fixture", nil)
	}

	return nil, nil
}

func TestBoundaryCapturesOneCurrentTimeAndRefreshesUnfinishedRetry(t *testing.T) {
	t.Parallel()

	scope := m.Scope{Key: "test:processing-clock", Kind: m.ScopeKindPersonal}
	store := &temporalStore{scope: scope, outcomes: make(map[string]m.OperationOutcome)}
	model := &temporalModel{failProfile: true}
	firstInstant := time.Date(2026, time.October, 13, 6, 0, 0, 0, time.FixedZone("fixture", 6*60*60))
	clockCalls := 0
	clock := func() time.Time {
		at := firstInstant.Add(time.Duration(clockCalls) * time.Hour)
		clockCalls++

		return at
	}

	processor, err := core.NewBoundaryProcessorWithClock(store, model, model, m.LegacyDerivationRef(), clock)
	if err != nil {
		t.Fatal(err)
	}

	occurred := time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

	boundary, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
		"close", m.BoundaryReasonClose, occurred)
	if err != nil {
		t.Fatal(err)
	}

	_, err = processor.ProcessBoundary(context.Background(), boundary)
	if err == nil {
		t.Fatal("failed profile accepted")
	}

	first := assertFirstBoundaryAttempt(t, store, firstInstant, occurred, clockCalls)

	model.failProfile = false

	_, err = processor.ProcessBoundary(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}

	assertRetriedBoundaryAttempt(t, store, model, first, clockCalls)
}

type replacementStore struct {
	temporalStore

	snapshot m.BoundarySnapshot
}

func (s *replacementStore) LoadBoundarySnapshot(_ context.Context,
	_ m.Scope,
	at time.Time) (m.BoundarySnapshot,
	error) {
	s.loads = append(s.loads, at)
	s.snapshot.Structural.Version = s.version
	s.snapshot.Eligible.Version = s.version

	return s.snapshot, nil
}

func (s *replacementStore) Commit(ctx context.Context, request m.CommitRequest) (m.OperationOutcome, error) {
	outcome, err := s.temporalStore.Commit(ctx, request)
	for _, scenario := range request.Scenarios {
		ref := m.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}
		outcome.Revisions = append(outcome.Revisions, ref)
		// Profile synthesis reloads after scenario success, using the same T.
		s.snapshot.Eligible.Scenarios = append(s.snapshot.Eligible.Scenarios, scenario)
		s.snapshot.Structural.Scenarios = []m.Scenario{scenario}
	}

	for _, profile := range request.Profiles {
		outcome.Revisions = append(outcome.Revisions,
			m.RevisionRef{ItemID: profile.Meta.ItemID,
				RevisionID: profile.Meta.RevisionID})
	}

	s.outcomes[request.OperationID] = outcome

	return outcome, err
}

type replacementModel struct{ t *testing.T }

func (s replacementModel) SynthesizeScenarios(_ context.Context,
	request m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate,
	error) {
	if len(request.View.Scenarios) != 0 || len(request.View.Profiles) != 0 {
		s.t.Error("expired heads entered model facts")
	}

	atom := request.View.Atoms[0]

	return []m.ScenarioCandidate{{TopicKey: "home", Title: "Home", Summary: "Current home",
		Atoms: []m.RevisionRef{{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}}}}, nil
}

func (s replacementModel) SynthesizeProfile(_ context.Context,
	request m.ProfileSynthesisRequest) (*m.ProfileCandidate,
	error) {
	if len(request.View.Scenarios) != 1 || len(request.View.Profiles) != 0 {
		s.t.Error("profile context did not exclude expired heads")
	}

	scenario := request.View.Scenarios[0]

	return &m.ProfileCandidate{Disposition: m.ProfileDispositionUpsert, Summary: "Current home",
		Scenarios: []m.RevisionRef{{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}}}, nil
}

func TestExpiredStructuralHeadsRetainSupersessionAndRevisionNumbers(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC)
	scope := m.Scope{Key: "test:structural-replacement", Kind: m.ScopeKindPersonal}
	snapshot := replacementSnapshot(t, scope, at)
	oldScenario := snapshot.Structural.Scenarios[0].Meta
	oldProfile := snapshot.Structural.Profiles[0].Meta
	store := &replacementStore{temporalStore: temporalStore{scope: scope,
		outcomes: make(map[string]m.OperationOutcome)},
		snapshot: snapshot}
	model := replacementModel{t: t}

	processor,
		err := core.NewBoundaryProcessorWithClock(store,
		model,
		model,
		m.LegacyDerivationRef(),
		func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}

	boundary, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
		"close", m.BoundaryReasonClose, at.Add(-24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	_, err = processor.ProcessBoundary(context.Background(), boundary)
	if err != nil {
		t.Fatal(err)
	}

	scenario := store.commits[0].Scenarios[0].Meta
	profile := store.commits[1].Profiles[0].Meta
	assertStructuralReplacement(t, scenario, oldScenario, store.commits[0].Transitions)
	assertStructuralReplacement(t, profile, oldProfile, store.commits[1].Transitions)
}

func assertStructuralReplacement(t *testing.T, created, prior m.RevisionMeta, transitions []m.RevisionTransition) {
	t.Helper()

	want := m.RevisionRef{ItemID: prior.ItemID, RevisionID: prior.RevisionID}
	if created.ItemID != prior.ItemID ||
		created.Revision != prior.Revision+1 ||
		created.Supersedes == nil ||
		*created.Supersedes != want {
		t.Errorf("replacement lost structural identity: prior=%+v new=%+v", prior, created)
	}

	if len(transitions) != 1 || transitions[0].Ref != want || transitions[0].To != m.RevisionStateSuperseded {
		t.Errorf("replacement does not retire prior active head: %+v", transitions)
	}
}

func replacementSnapshot(t *testing.T, scope m.Scope, at time.Time) m.BoundarySnapshot {
	t.Helper()

	atomItem, err := m.AtomItemID(scope, m.AtomCategoryFact, "Current home")
	if err != nil {
		t.Fatal(err)
	}

	raw := m.Provenance{RawSources: []m.SourceRef{{Scope: scope,
		ExportID:       "source",
		ConversationID: "c",
		SourceTurnID:   "turn"}}}
	atomMeta := replacementMeta(t, scope, m.DerivedKindAtom, atomItem, 1, raw, []string{"fact", "Current home", "new"}, at)
	atom := m.Atom{Meta: atomMeta, Category: m.AtomCategoryFact, Text: "Current home", Relation: m.CandidateRelationNew}
	parents := m.Provenance{ParentRevisions: []m.RevisionRef{{ItemID: atomItem, RevisionID: atomMeta.RevisionID}}}

	scenarioItem, err := m.ScenarioItemID(scope, "home")
	if err != nil {
		t.Fatal(err)
	}

	scenarioMeta := replacementMeta(t,
		scope,
		m.DerivedKindScenario,
		scenarioItem,
		4,
		parents,
		[]string{"home",
			"Home",
			"Old home"},
		at)
	scenario := m.Scenario{Meta: scenarioMeta, TopicKey: "home", Title: "Home", Summary: "Old home",
		Temporal: &m.Temporal{ObservedAt: at.Add(-time.Hour), ExpiresAt: &at}}

	profileItem, err := m.ProfileItemID(scope)
	if err != nil {
		t.Fatal(err)
	}

	profileMeta := replacementMeta(t, scope, m.DerivedKindProfile, profileItem, 7, parents, []string{"Old profile"}, at)
	profile := m.Profile{Meta: profileMeta, Summary: "Old profile", Temporal: scenario.Temporal}

	return m.BoundarySnapshot{
		Structural: m.ScopeSnapshot{SchemaVersion: m.DerivedSchemaVersionV1, Scope: scope,
			Atoms: []m.Atom{atom}, Scenarios: []m.Scenario{scenario}, Profiles: []m.Profile{profile}},
		Eligible: m.ScopeView{SchemaVersion: m.DerivedSchemaVersionV1, Scope: scope, Atoms: []m.Atom{atom}},
	}
}

func replacementMeta(t *testing.T, scope m.Scope, kind m.DerivedKind, item string, revision uint64,
	provenance m.Provenance, content []string, at time.Time) m.RevisionMeta {
	t.Helper()

	id, err := m.DerivedRevisionID(scope, item, "prior-operation", content, provenance, nil)
	if err != nil {
		t.Fatal(err)
	}

	return m.RevisionMeta{SchemaVersion: m.DerivedSchemaVersionV1, Kind: kind, ItemID: item, RevisionID: id,
		Revision:    revision,
		OperationID: "prior-operation",
		Scope:       scope,
		State:       m.RevisionStateActive,
		Provenance:  provenance,
		CreatedAt:   at.Add(-time.Hour)}
}

func assertFirstBoundaryAttempt(t *testing.T,
	store *temporalStore,
	firstInstant,
	occurred time.Time,
	clockCalls int) time.Time {
	t.Helper()

	if len(store.loads) != 2 || len(store.commits) != 1 {
		t.Fatalf("snapshot calls=%d commits=%d", len(store.loads), len(store.commits))
	}

	first := store.loads[0]
	if clockCalls != 1 ||
		first != firstInstant.UTC() ||
		first.Location() != time.UTC ||
		first != store.loads[1] ||
		first != store.commits[0].ProcessingAt ||
		!first.After(occurred) {
		t.Errorf("stages did not share current processing time: loads=%v commit=%v",
			store.loads,
			store.commits[0].ProcessingAt)
	}

	return first
}

func assertRetriedBoundaryAttempt(t *testing.T,
	store *temporalStore,
	model *temporalModel,
	first time.Time,
	clockCalls int) {
	t.Helper()

	if len(store.loads) != 3 || len(store.commits) != 2 || model.scenarioCalls != 1 || model.profileCalls != 2 {
		t.Fatalf("unfinished retry loads=%v calls=%d/%d", store.loads, model.scenarioCalls, model.profileCalls)
	}

	if clockCalls != 2 || store.loads[2] != first.Add(time.Hour) || store.commits[1].ProcessingAt != store.loads[2] {
		t.Errorf("retry did not refresh processing time: %v", store.loads)
	}
}

type ineligibleParentModel struct {
	ref    m.RevisionRef
	target *m.RevisionRef
}

func (s ineligibleParentModel) SynthesizeScenarios(context.Context,
	m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	return []m.ScenarioCandidate{{TopicKey: "home", Title: "Home", Summary: "Current home",
		Atoms: []m.RevisionRef{s.ref}, Supersedes: s.target}}, nil
}

func (ineligibleParentModel) SynthesizeProfile(context.Context,
	m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	return nil, nil
}

func TestBoundaryRejectsIneligibleStructuralGroundingParent(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC)
	scope := m.Scope{Key: "test:ineligible-parent", Kind: m.ScopeKindPersonal}
	snapshot := replacementSnapshot(t, scope, at)
	atom := snapshot.Structural.Atoms[0]
	snapshot.Eligible.Atoms = nil
	snapshot.Structural.Scenarios = nil
	snapshot.Structural.Profiles = nil
	store := &replacementStore{temporalStore: temporalStore{scope: scope, outcomes: make(map[string]m.OperationOutcome)},
		snapshot: snapshot}
	model := ineligibleParentModel{ref: m.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}}

	processor, err := core.NewBoundaryProcessorWithClock(store, model, model, m.LegacyDerivationRef(),
		func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}

	boundary, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
		"close", m.BoundaryReasonClose, at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	_, err = processor.ProcessBoundary(context.Background(), boundary)

	code, _, ok := m.ClassifyError(err)
	if !ok || code != m.CodeInvalidDerived || len(store.commits) != 0 {
		t.Errorf("ineligible grounding error=%v committed stages=%d", err, len(store.commits))
	}
}

type conflictingTargetModel struct {
	ineligibleParentModel

	profile bool
}

func (s conflictingTargetModel) SynthesizeScenarios(ctx context.Context,
	request m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	if s.profile {
		return nil, nil
	}

	return s.ineligibleParentModel.SynthesizeScenarios(ctx, request)
}

func (s conflictingTargetModel) SynthesizeProfile(context.Context,
	m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	if !s.profile {
		return nil, nil
	}

	return &m.ProfileCandidate{Disposition: m.ProfileDispositionUpsert, Summary: "Current home",
		Atoms: []m.RevisionRef{s.ref}, Supersedes: s.target}, nil
}

func TestBoundaryRejectsConflictingSuppliedStructuralTarget(t *testing.T) {
	t.Parallel()

	for _, stage := range []string{"scenario", "profile"} {
		t.Run(stage, func(t *testing.T) {
			at := time.Date(2026, time.October, 13, 0, 0, 0, 0, time.UTC)
			scope := m.Scope{Key: "test:conflicting-target", Kind: m.ScopeKindPersonal}
			snapshot := replacementSnapshot(t, scope, at)
			atom := snapshot.Structural.Atoms[0]
			store := &replacementStore{temporalStore: temporalStore{scope: scope, outcomes: make(map[string]m.OperationOutcome)},
				snapshot: snapshot}
			target := m.RevisionRef{ItemID: "foreign-item", RevisionID: "foreign-revision"}
			model := conflictingTargetModel{profile: stage == "profile", ineligibleParentModel: ineligibleParentModel{
				ref: m.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}, target: &target}}

			processor, err := core.NewBoundaryProcessorWithClock(store, model, model, m.LegacyDerivationRef(),
				func() time.Time { return at })
			if err != nil {
				t.Fatal(err)
			}

			boundary, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"},
				"close", m.BoundaryReasonClose, at)
			if err != nil {
				t.Fatal(err)
			}

			_, err = processor.ProcessBoundary(context.Background(), boundary)
			code, _, ok := m.ClassifyError(err)

			wantCommits := 0
			if stage == "profile" {
				wantCommits = 1
			}

			if !ok || code != m.CodeInvalidDerived || len(store.commits) != wantCommits {
				t.Errorf("conflicting target error=%v committed stages=%d want %d", err, len(store.commits), wantCommits)
			}
		})
	}
}

func (s *temporalStore) VerifyCanonicalBoundary(_ context.Context, input m.Boundary,
) (m.BoundaryIdentityResult, error) {
	expected, err := m.NewCanonicalBoundaryIdentityCommit(input)
	if err != nil {
		return m.BoundaryIdentityResult{}, err
	}

	for _, commit := range s.commits {
		proof := commit.BoundaryIdentity
		if proof == nil || proof.Certificate.ExportID != input.ExportID {
			continue
		}

		if proof.Certificate != expected.Certificate {
			return m.BoundaryIdentityResult{}, m.PermanentError(m.CodeConflict, "changed fixture boundary", nil)
		}

		return m.BoundaryIdentityResult{Certified: true}, nil
	}

	return m.BoundaryIdentityResult{SafelyNew: true}, nil
}
