package canonicalcore_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	core "github.com/baldaworks/boyan/internal/canonicalcore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	read "github.com/baldaworks/boyan/internal/memoryread"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type inheritanceModel struct {
	failProfile                 bool
	scenarioCalls, profileCalls int
	scenario                    m.RevisionRef
}

func (i *inheritanceModel) SynthesizeScenarios(_ context.Context,
	request m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	i.scenarioCalls++

	var selected []m.RevisionRef

	for _, atom := range request.View.Atoms {
		if atom.Text == "Parent A" || atom.Text == "Parent B" {
			selected = append(selected, m.RevisionRef{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID})
		}
	}

	if len(selected) != 2 {
		return nil, fmt.Errorf("fixture expected two selected semantic parents, got %d", len(selected))
	}

	return []m.ScenarioCandidate{{TopicKey: "shared", Title: "Shared", Summary: "shared retained context",
		Atoms: selected}}, nil
}

func (i *inheritanceModel) SynthesizeProfile(_ context.Context,
	request m.ProfileSynthesisRequest) (*m.ProfileCandidate, error) {
	i.profileCalls++

	if len(request.View.Scenarios) == 1 {
		scenario := request.View.Scenarios[0]
		i.scenario = m.RevisionRef{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}
	}

	if i.failProfile {
		i.failProfile = false

		return nil, m.RetryableError(m.CodeModelFailure, "fixture profile failure", nil)
	}

	return &m.ProfileCandidate{Disposition: m.ProfileDispositionUpsert, Summary: "shared retained profile",
		Scenarios: []m.RevisionRef{i.scenario}}, nil
}

type inheritanceFixture struct {
	store     *b.CanonicalStore
	reader    *b.CanonicalReader
	processor *boundary.BoundaryProcessor
	model     *inheritanceModel
	export    m.Boundary
	now       time.Time
	from      time.Time
	until     time.Time
	expiry    time.Time
}

func newInheritanceFixture(t *testing.T, failProfile bool) *inheritanceFixture {
	t.Helper()
	store, reader := newModelRetryStore(t)
	at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	f := &inheritanceFixture{store: store, reader: reader, now: at,
		from: at.Add(-2 * time.Hour), until: at.Add(3 * time.Hour), expiry: at.Add(2 * time.Hour),
		model: &inheritanceModel{failProfile: failProfile}}
	turn := seedInheritanceParents(t, store, at)
	adapter, err := ops.NewCanonicalBoundaryStore(store, reader)
	requireNoError(t, err, "boundary adapter: %v")
	f.processor, err = boundary.NewBoundaryProcessorWithClock(adapter, f.model, f.model,
		testProcessorDerivation(), func() time.Time { return f.now })
	requireNoError(t, err, "boundary processor: %v")
	f.export, err = m.NewBoundary(turn.Scope, turn.Conversation, "inheritance", m.BoundaryReasonClose,
		at.Add(-time.Hour))
	requireNoError(t, err, "boundary: %v")

	return f
}

func seedInheritanceParents(t *testing.T, store *b.CanonicalStore, at time.Time) m.Turn {
	t.Helper()

	extractor := canonicalExtractorFunc(func(ctx context.Context,
		request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
		candidates, err := extractTestStateCandidate(ctx, request)
		if err == nil {
			candidates[0].Predicate = request.Turn.SourceTurnID

			candidates[0].Memory.Temporal = inheritanceParentTemporal(request.Turn.SourceTurnID, at)
			if request.Turn.SourceTurnID == "Parent A" {
				candidates[0].Memory.Retention = m.RetentionClassEphemeral
			}
		}

		return candidates, err
	})
	processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "native processor: %v")

	var turn m.Turn
	for _, name := range []string{"Parent A", "Parent B", "Unrelated"} {
		turn, err = m.NewTurn(m.Scope{Key: "test:inheritance", Kind: m.ScopeKindPersonal},
			m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
			name, at.Add(-time.Hour), name, "OK")
		requireNoError(t, err, "turn: %v")
		_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
		requireNoError(t, err, "seed native: %v")
	}

	return turn
}

func inheritanceParentTemporal(name string, at time.Time) m.Temporal {
	from, until, expiry := at.Add(-3*time.Hour), at.Add(4*time.Hour), at.Add(4*time.Hour)
	if name == "Parent B" {
		from, until, expiry = at.Add(-2*time.Hour), at.Add(3*time.Hour), at.Add(2*time.Hour)
	}

	if name == "Unrelated" {
		expiry = at.Add(time.Hour)
	}

	return m.Temporal{ObservedAt: at.Add(-time.Hour), ValidFrom: &from, ValidUntil: &until, ExpiresAt: &expiry}
}

func TestBoundaryInheritsSelectedParentBoundsThroughScenarioAndProfile(t *testing.T) {
	t.Parallel()
	f := newInheritanceFixture(t, false)
	outcome, err := f.processor.ProcessBoundary(context.Background(), f.export)
	requireNoError(t, err, "boundary synthesis: %v")

	for _, stage := range []m.OperationOutcome{outcome.Scenarios, outcome.Profile} {
		assertInheritedStage(t, f, stage)
	}

	service, err := read.NewRecallService(f.reader, nil)
	requireNoError(t, err, "recall service: %v")

	for _, test := range []struct {
		at   time.Time
		want int
	}{
		{at: f.now, want: 2},
		{at: f.now.Add(time.Hour), want: 2},
		{at: f.expiry, want: 0},
	} {
		response, searchErr := service.Search(context.Background(), m.RecallRequest{
			Scope: f.export.Scope, Query: "shared", Limit: 10, AsOf: &test.at,
		})
		requireNoError(t, searchErr, "search: %v")

		if len(response.Results) != test.want {
			t.Fatalf("at %s results=%d, want %d", test.at, len(response.Results), test.want)
		}
	}
}

func assertInheritedStage(t *testing.T, f *inheritanceFixture, stage m.OperationOutcome) {
	t.Helper()

	record, found, err := f.store.LoadCanonicalOperation(context.Background(), f.export.Scope, stage.OperationID)
	requireNoError(t, err, "stage operation: %v")

	if !found || len(record.Outcome.RevisionIDs) != 1 {
		t.Fatal("expected durable synthesized stage revision")
	}

	revisions, err := f.store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
		Scope: f.export.Scope, RevisionIDs: record.Outcome.RevisionIDs,
	})
	requireNoError(t, err, "persisted stage revision: %v")

	want := m.Temporal{ObservedAt: f.now, ValidFrom: &f.from, ValidUntil: &f.until, ExpiresAt: &f.expiry}
	if len(revisions) != 1 || !reflect.DeepEqual(revisions[0].Temporal, want) ||
		revisions[0].Retention != m.RetentionClassEphemeral {
		t.Fatalf("synthesis lost selected ancestor bounds/retention: %+v", revisions)
	}
}

func TestExpiredReplayedScenarioCannotGroundUnfinishedProfile(t *testing.T) {
	t.Parallel()
	f := newInheritanceFixture(t, true)

	partial, err := f.processor.ProcessBoundary(context.Background(), f.export)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeModelFailure {
		t.Fatalf("first profile fixture should fail after scenario commit: %v", err)
	}

	before, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "partial state: %v")

	f.now = f.expiry

	retried, err := f.processor.ProcessBoundary(context.Background(), f.export)
	if code, class, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived || class != m.ErrorClassPermanent {
		t.Fatalf("expired scenario grounded unfinished profile: %v", err)
	}

	assertOnlyUnfinishedProfileRan(t, f, partial, retried)

	assertRejectedInheritanceProfile(t, f, before)
}

func assertOnlyUnfinishedProfileRan(t *testing.T, f *inheritanceFixture, partial, retried m.BoundaryOutcome) {
	t.Helper()

	if f.model.scenarioCalls != 1 || f.model.profileCalls != 2 ||
		!reflect.DeepEqual(partial.Scenarios, retried.Scenarios) {
		t.Fatal("unfinished retry did not preserve scenario replay and run only profile")
	}
}

func assertRejectedInheritanceProfile(t *testing.T, f *inheritanceFixture, before m.ScopeState) {
	t.Helper()

	after, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "rejected retry state: %v")
	profileID, err := m.ProcessingOperationID(m.OperationStageProfile, f.export.ExportID, testProcessorDerivation())
	requireNoError(t, err, "profile id: %v")
	_, found, err := f.store.LoadCanonicalOperation(context.Background(), f.export.Scope, profileID)
	requireNoError(t, err, "profile outcome: %v")

	if before != after || found {
		t.Fatal("expired grounding changed state or persisted profile outcome")
	}
}
