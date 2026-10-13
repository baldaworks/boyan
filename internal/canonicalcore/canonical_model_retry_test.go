package canonicalcore_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	derive "github.com/baldaworks/boyan/internal/structuredderive"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

type modelRetryInvoker struct {
	raw   string
	calls int
}

func (i *modelRetryInvoker) Invoke(context.Context, derive.StructuredInvocation) ([]byte, error) {
	i.calls++

	return []byte(i.raw), nil
}
func (*modelRetryInvoker) Close(context.Context) error { return nil }

func newModelRetryStore(t *testing.T) (*b.CanonicalStore, *b.CanonicalReader) {
	t.Helper()
	store, err := b.Open(t.TempDir())
	requireNoError(t, err, "open: %v")
	t.Cleanup(func() { requireNoError(t, store.Close(), "close: %v") })

	reader, err := b.NewCanonicalReader(store)
	requireNoError(t, err, "reader: %v")

	return store, reader
}

func modelRetryTurn(t *testing.T) m.Turn {
	t.Helper()

	turn, err := m.NewTurn(m.Scope{Key: "test:model-retry", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC), "I live in Bishkek", "OK")
	requireNoError(t, err, "turn: %v")

	return turn
}

func TestMalformedModelOutputCannotCommitAndCorrectedTurnCanRetry(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{`{}`, `null`, `{"output":null}`, `{"output":[],"unexpected":"ignored"}`} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			store, reader := newModelRetryStore(t)
			turn := modelRetryTurn(t)
			invoker := &modelRetryInvoker{raw: raw}
			deriver, err := derive.NewDeriver(invoker)
			requireNoError(t, err, "deriver: %v")
			processor, err := core.NewCanonicalTurnProcessor(store, deriver, m.PolicyRegistry{Version: "policy-v1"})
			requireNoError(t, err, "processor: %v")
			operationID, err := m.CanonicalSemanticOperationID(turn.ExportID, testProcessorDerivation())
			requireNoError(t, err, "operation identity: %v")
			_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())

			code, class, ok := m.ClassifyError(err)
			if !ok || code != m.CodeInvalidDerived || class != m.ErrorClassPermanent {
				t.Errorf("malformed output error=%v; want permanent invalid_derived", err)
			}

			assertRejectedTurnState(t, store, turn.Scope, operationID)
			invoker.raw = correctedModelOutput(t, turn)
			outcome, err := processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
			requireNoError(t, err, "corrected same-operation retry: %v")
			assertCorrectedTurnState(t, store, reader, turn, operationID, outcome)

			if invoker.calls != 2 {
				t.Errorf("model calls=%d, want failed call and corrected retry", invoker.calls)
			}
		})
	}
}

func correctedModelOutput(t *testing.T, turn m.Turn) string {
	t.Helper()

	candidates, err := extractTestStateCandidate(context.Background(), m.CanonicalExtractionRequest{Turn: turn})
	requireNoError(t, err, "candidate: %v")
	raw, err := json.Marshal(struct {
		Output []m.SemanticCandidate `json:"output"`
	}{Output: candidates})
	requireNoError(t, err, "model fixture: %v")

	return string(raw)
}

func assertRejectedTurnState(t *testing.T, store *b.CanonicalStore, scope m.Scope, operationID string) {
	t.Helper()

	state, err := store.LoadScopeState(context.Background(), scope)
	requireNoError(t, err, "scope state: %v")

	if state.Version != 0 || state.ChangeSeq != 0 {
		t.Errorf("failed output advanced state: %+v", state)
	}

	changes, err := store.ScanScopeChanges(context.Background(), scope, 0, 10)
	requireNoError(t, err, "scope changes: %v")

	if len(changes) != 0 {
		t.Errorf("failed output committed changes/revisions: %+v", changes)
	}

	heads, err := store.ScanActiveHeads(context.Background(), m.ActiveHeadScanRequest{Scope: scope, Limit: 10})
	requireNoError(t, err, "active revisions: %v")

	if len(heads) != 0 {
		t.Errorf("failed output created revisions: %+v", heads)
	}

	_, found, err := store.LoadCanonicalOperation(context.Background(), scope, operationID)
	requireNoError(t, err, "operation: %v")

	if found {
		t.Error("failed output recorded a successful operation")
	}
}

func assertCorrectedTurnState(t *testing.T, store *b.CanonicalStore, reader *b.CanonicalReader,
	turn m.Turn, operationID string, outcome m.CanonicalMutationOutcome) {
	t.Helper()

	if outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 || len(outcome.RevisionIDs) != 1 {
		t.Fatalf("corrected output outcome=%+v; want first committed memory", outcome)
	}

	records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, outcome.RevisionIDs)
	requireNoError(t, err, "persisted memory: %v")

	if len(records) != 1 || records[0].Text != turn.Messages[0].Text {
		t.Errorf("corrected memory=%+v", records)
	}

	operation, found, err := store.LoadCanonicalOperation(context.Background(), turn.Scope, operationID)
	requireNoError(t, err, "successful operation: %v")

	if !found || !reflect.DeepEqual(operation.Outcome.RevisionIDs, outcome.RevisionIDs) {
		t.Errorf("corrected same operation not persisted: found=%v record=%+v", found, operation)
	}
}

func TestExplicitEmptyModelOutputCommitsAndReplays(t *testing.T) {
	t.Parallel()
	store, _ := newModelRetryStore(t)
	turn := modelRetryTurn(t)
	invoker := &modelRetryInvoker{raw: `{"output":[]}`}
	deriver, err := derive.NewDeriver(invoker)
	requireNoError(t, err, "deriver: %v")
	processor, err := core.NewCanonicalTurnProcessor(store, deriver, m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	outcome, err := processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "explicit empty: %v")

	if outcome.ScopeVersion != 1 || outcome.ChangeSeq != 1 || len(outcome.RevisionIDs) != 0 {
		t.Fatalf("empty output outcome=%+v", outcome)
	}

	operationID, err := m.CanonicalSemanticOperationID(turn.ExportID, testProcessorDerivation())
	requireNoError(t, err, "operation identity: %v")
	operation, found, err := store.LoadCanonicalOperation(context.Background(), turn.Scope, operationID)
	requireNoError(t, err, "empty operation: %v")

	if !found || len(operation.Outcome.RevisionIDs) != 0 {
		t.Fatalf("empty operation=%+v, found=%v", operation, found)
	}

	replay, err := processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "empty replay: %v")

	if !reflect.DeepEqual(replay, outcome) || invoker.calls != 1 {
		t.Errorf("empty replay=%+v calls=%d; original=%+v", replay, invoker.calls, outcome)
	}
}
