package boundarycore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/boundarycore"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type lookupRacingIdentityStore struct {
	*temporalStore

	denied bool
	seen   m.Boundary
}

func (s *lookupRacingIdentityStore) VerifyCanonicalBoundary(_ context.Context, input m.Boundary,
) (m.BoundaryIdentityResult, error) {
	s.seen = input
	if s.denied {
		return m.BoundaryIdentityResult{}, m.PermanentError(m.CodeConflict, "competing boundary committed", nil)
	}

	return m.BoundaryIdentityResult{SafelyNew: true}, nil
}

func (s *lookupRacingIdentityStore) LookupOperation(_ context.Context, lookup m.OperationLookup,
) (m.OperationLookupResult, error) {
	s.denied = true

	return m.OperationLookupResult{Found: true, Outcome: m.OperationOutcome{
		SchemaVersion: m.DerivedSchemaVersionV1, OperationID: lookup.OperationID, Stage: lookup.Stage,
		Scope: lookup.Scope, ScopeVersion: 1}}, nil
}

func TestBoundaryReplayRechecksProofAfterStageLookup(t *testing.T) {
	scope := m.Scope{Key: "replay:lookup-race", Kind: m.ScopeKindPersonal}
	store := &lookupRacingIdentityStore{temporalStore: &temporalStore{scope: scope}}
	model := &temporalModel{}

	processor, err := core.NewBoundaryProcessor(store, model, model, m.LegacyDerivationRef())
	if err != nil {
		t.Fatal(err)
	}

	input, err := m.NewBoundary(scope, m.ConversationRef{ConversationID: "c", AgentConversationID: "a"}, "close",
		m.BoundaryReasonClose, time.Date(2026, time.October, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	input.TransitionID = " close "
	input.OccurredAt = input.OccurredAt.In(time.FixedZone("offset", 3600))
	outcome, err := processor.ProcessBoundary(context.Background(), input)

	assertReplayRaceRejected(t, store, model, outcome, err)

	if store.seen.TransitionID != "close" || store.seen.OccurredAt.Location() != time.UTC {
		t.Fatalf("unnormalized input=%+v", store.seen)
	}
}

func assertReplayRaceRejected(
	t *testing.T, store *lookupRacingIdentityStore, model *temporalModel, outcome m.BoundaryOutcome, err error,
) {
	t.Helper()

	code, class, ok := m.ClassifyError(err)
	if !ok || code != m.CodeConflict || class != m.ErrorClassPermanent {
		t.Fatalf("lookup bypassed immutable proof: %v", err)
	}

	if outcome.Scenarios.OperationID != "" {
		t.Fatal("replay race exposed another input scenario outcome")
	}

	if model.scenarioCalls != 0 || model.profileCalls != 0 || len(store.commits) != 0 {
		t.Fatal("replay race invoked model or commit")
	}
}
