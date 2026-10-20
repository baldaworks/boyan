package canonicalcore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type boundaryInvoker struct {
	calls int
	count int
	bytes int
}

func (i *boundaryInvoker) Invoke(_ context.Context, invocation derive.StructuredInvocation) ([]byte, error) {
	i.calls++
	i.bytes = len(invocation.InputJSON)

	var request m.ScenarioSynthesisRequest
	if err := json.Unmarshal(invocation.InputJSON, &request); err != nil {
		return nil, err
	}

	i.count = len(request.View.Atoms) + len(request.View.Scenarios) + len(request.View.Profiles)

	return []byte(`{"output":[]}`), nil
}
func (*boundaryInvoker) Close(context.Context) error { return nil }

func TestNativeBoundarySynthesisBeyondSearchPage(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		var store coreOwner

		var reader coreReader

		var err error

		if factory != nil {
			store, reader = factory(t, t.TempDir())
		} else {
			legacy, openErr := b.Open(t.TempDir())
			requireNoError(t, openErr, "store: %v")

			store = legacy
			reader, err = b.NewCanonicalReader(legacy)
			requireNoError(t, err, "reader: %v")
		}

		t.Cleanup(func() { requireNoError(t, store.Close(), "close: %v") })

		scope := m.Scope{Key: "test:growth", Kind: m.ScopeKindPersonal}
		conversation := m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
		extractor := canonicalExtractorFunc(func(ctx context.Context,
			request m.CanonicalExtractionRequest) ([]m.SemanticCandidate,
			error) {
			candidates, extractErr := extractTestStateCandidate(ctx, request)
			if extractErr == nil {
				candidates[0].Predicate = request.Turn.Messages[0].Text
			}

			return candidates, extractErr
		})
		processor, err := core.NewCanonicalTurnProcessor(store, extractor, m.PolicyRegistry{Version: "policy-v1"})
		requireNoError(t, err, "processor: %v")

		completed := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)

		for i := range 257 {
			id := fmt.Sprintf("turn-%03d", i)
			turn, turnErr := m.NewTurn(scope, conversation, id, completed, id, "OK")
			requireNoError(t, turnErr, "turn: %v")
			_, turnErr = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
			requireNoError(t, turnErr, "process: %v")
		}

		view, err := reader.LoadCanonicalBoundaryView(context.Background(), scope)
		requireNoError(t, err, "boundary view: %v")

		if len(view.Snapshot.Atoms) != 257 {
			t.Fatalf("omitted atoms: %d", len(view.Snapshot.Atoms))
		}

		assertStructuredBoundaryBudget(t, view.Snapshot, conversation, completed)
	})
}

func assertStructuredBoundaryBudget(t *testing.T,
	snapshot m.ScopeSnapshot,
	conversation m.ConversationRef,
	completed time.Time) {
	t.Helper()

	invoker := &boundaryInvoker{}
	deriver, err := derive.NewDeriver(invoker)
	requireNoError(t, err, "deriver: %v")
	boundary, err := m.NewBoundary(snapshot.Scope, conversation, "boundary", m.BoundaryReasonClose, completed)
	requireNoError(t, err, "boundary: %v")

	request := m.ScenarioSynthesisRequest{SchemaVersion: m.DerivedSchemaVersionV1,
		Derivation: testProcessorDerivation(),
		Boundary:   boundary,
		View: m.ScopeView{SchemaVersion: snapshot.SchemaVersion,
			Scope:     snapshot.Scope,
			Version:   snapshot.Version,
			Atoms:     snapshot.Atoms,
			Scenarios: snapshot.Scenarios,
			Profiles:  snapshot.Profiles}}
	_, err = deriver.SynthesizeScenarios(context.Background(), request)
	requireNoError(t, err, "synthesis: %v")
	t.Logf("257 native records complete request: %d bytes", invoker.bytes)

	if invoker.calls != 1 || invoker.bytes > m.MaxDerivedTurnTextBytes || invoker.count != 257 {
		t.Fatalf("invalid provider input: calls=%d bytes=%d count=%d", invoker.calls, invoker.bytes, invoker.count)
	}
}
