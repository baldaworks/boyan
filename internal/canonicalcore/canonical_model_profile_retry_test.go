package canonicalcore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type profileRetryInvoker struct {
	invalidProfile bool
	scenarioCalls  int
	profileCalls   int
}

func (i *profileRetryInvoker) Invoke(_ context.Context, request derive.StructuredInvocation) ([]byte, error) {
	switch request.Stage {
	case string(m.OperationStageScenarios):
		i.scenarioCalls++

		var input m.ScenarioSynthesisRequest
		if err := json.Unmarshal(request.InputJSON, &input); err != nil {
			return nil, err
		}

		if len(input.View.Atoms) != 1 {
			return nil, fmt.Errorf("scenario fixture needs one native parent, got %d", len(input.View.Atoms))
		}

		atom := input.View.Atoms[0]

		return json.Marshal(map[string]any{"output": []m.ScenarioCandidate{{
			TopicKey: "home", Title: "Home", Summary: "Lives in Bishkek",
			Atoms: []m.RevisionRef{{ItemID: atom.Meta.ItemID, RevisionID: atom.Meta.RevisionID}},
		}}})
	case string(m.OperationStageProfile):
		i.profileCalls++
		if i.invalidProfile {
			return []byte(`{"output":{"disposition":"skip"},"unexpected":true}`), nil
		}

		var input m.ProfileSynthesisRequest
		if err := json.Unmarshal(request.InputJSON, &input); err != nil {
			return nil, err
		}

		if len(input.View.Scenarios) != 1 {
			return nil, fmt.Errorf("profile fixture needs one scenario, got %d", len(input.View.Scenarios))
		}

		scenario := input.View.Scenarios[0]

		return json.Marshal(map[string]any{"output": m.ProfileCandidate{
			Disposition: m.ProfileDispositionUpsert, Summary: "Lives in Bishkek",
			Scenarios: []m.RevisionRef{{ItemID: scenario.Meta.ItemID, RevisionID: scenario.Meta.RevisionID}},
		}})
	default:
		return nil, fmt.Errorf("unexpected fixture stage %q", request.Stage)
	}
}

func (*profileRetryInvoker) Close(context.Context) error { return nil }

func TestMalformedProfilePreservesScenarioAndRetriesOnlyProfile(t *testing.T) {
	t.Parallel()
	reader, _, turn, native, store := nativeFlow(t)
	adapter, err := ops.NewCanonicalBoundaryStore(store, reader)
	requireNoError(t, err, "adapter: %v")

	invoker := &profileRetryInvoker{invalidProfile: true}
	deriver, err := derive.NewDeriver(invoker)
	requireNoError(t, err, "deriver: %v")
	processor, err := boundary.NewBoundaryProcessor(adapter, deriver, deriver, testProcessorDerivation())
	requireNoError(t, err, "processor: %v")
	export, err := m.NewBoundary(turn.Scope, turn.Conversation, "close", m.BoundaryReasonClose, time.Now().UTC())
	requireNoError(t, err, "boundary: %v")
	partial, err := processor.ProcessBoundary(context.Background(), export)

	code, class, classified := m.ClassifyError(err)
	if !classified || code != m.CodeInvalidDerived || class != m.ErrorClassPermanent {
		t.Fatalf("malformed profile accepted or misclassified: %v", err)
	}

	assertPartialModelStages(t, store, reader, export, partial, native)

	invoker.invalidProfile = false
	retried, err := processor.ProcessBoundary(context.Background(), export)
	requireNoError(t, err, "corrected same-boundary retry: %v")

	if invoker.scenarioCalls != 1 || invoker.profileCalls != 2 || len(retried.Profile.Revisions) != 1 {
		t.Fatalf("retry did not finish only profile: calls=%d/%d outcome=%+v",
			invoker.scenarioCalls, invoker.profileCalls, retried)
	}

	if !reflect.DeepEqual(retried.Scenarios, partial.Scenarios) {
		t.Fatal("profile retry changed durable scenario outcome")
	}

	state, err := store.LoadScopeState(context.Background(), turn.Scope)
	requireNoError(t, err, "retry state: %v")

	if state.Version != native.ScopeVersion+2 || state.ChangeSeq != native.ChangeSeq+2 {
		t.Fatalf("retry counters = %+v; native = %+v", state, native)
	}
}

func assertPartialModelStages(t *testing.T, store *b.CanonicalStore, reader *b.CanonicalReader,
	export m.Boundary, partial m.BoundaryOutcome, native m.CanonicalMutationOutcome) {
	t.Helper()

	ctx := context.Background()
	state, err := store.LoadScopeState(ctx, export.Scope)
	requireNoError(t, err, "partial state: %v")

	if state.Version != native.ScopeVersion+1 || state.ChangeSeq != native.ChangeSeq+1 {
		t.Fatalf("failed profile changed counters beyond successful scenario: %+v", state)
	}

	scenario, found, err := store.LoadCanonicalOperation(ctx, export.Scope, partial.Scenarios.OperationID)
	requireNoError(t, err, "scenario operation: %v")

	if !found || len(scenario.Outcome.RevisionIDs) != 1 {
		t.Fatal("successful scenario operation was not durable")
	}

	profileID, err := m.ProcessingOperationID(m.OperationStageProfile, export.ExportID, testProcessorDerivation())
	requireNoError(t, err, "profile operation id: %v")
	_, found, err = store.LoadCanonicalOperation(ctx, export.Scope, profileID)
	requireNoError(t, err, "profile operation: %v")

	if found || len(partial.Scenarios.Revisions) != 1 || len(partial.Profile.Revisions) != 0 {
		t.Fatalf("invalid profile left successful outcome/revision: found=%v partial=%+v", found, partial)
	}

	view, err := reader.LoadCanonicalBoundaryView(ctx, export.Scope)
	requireNoError(t, err, "persisted view: %v")

	if len(view.Snapshot.Scenarios) != 1 || len(view.Snapshot.Profiles) != 0 {
		t.Fatalf("partial stored revisions: scenarios=%d profiles=%d",
			len(view.Snapshot.Scenarios), len(view.Snapshot.Profiles))
	}
}
