package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	ordered "github.com/baldaworks/boyan/internal/orderedingest"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type failingReceiptStore struct {
	coreOwner

	remaining int
}

func (s *failingReceiptStore) SaveIngestSequence(ctx context.Context, record m.IngestSequenceRecord) error {
	if s.remaining > 0 {
		s.remaining--

		return m.RetryableError(m.CodeStoreFailure, "injected receipt save failure", nil)
	}

	return s.coreOwner.SaveIngestSequence(ctx, record)
}

func receiptRecoveryRuntime(
	t *testing.T, store coreOwner, failures int, calls *int,
) *ordered.SequencedIngestRuntime {
	t.Helper()

	processor, err := core.NewCanonicalTurnProcessor(store, canonicalExtractorFunc(
		func(ctx context.Context, request m.CanonicalExtractionRequest) ([]m.SemanticCandidate, error) {
			*calls++
			if *calls > 1 {
				return nil, m.RetryableError(m.CodeModelFailure, "provider unavailable after commit", nil)
			}

			return extractTestStateCandidate(ctx, request)
		}), m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	runtime, err := ordered.NewSequencedIngestRuntime(&failingReceiptStore{coreOwner: store, remaining: failures},
		store, processor, testProcessorDerivation(), nil, nil, nil, ordered.SequencedIngestConfig{})
	requireNoError(t, err, "ordered runtime: %v")
	requireNoError(t, runtime.Start(context.Background()), "start: %v")

	return runtime
}

func TestCommittedCanonicalTurnRecoversMissingReceiptWithoutModel(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, reopen := range []bool{false, true} {
			name := "same-process-multiple-failures"
			if reopen {
				name = "reopened-store"
			}

			t.Run(name, func(t *testing.T) { testCommittedReceiptRecovery(t, reopen, factory) })
		}
	})
}

func testCommittedReceiptRecovery(t *testing.T, reopen bool, factory coreFactory) {
	t.Helper()

	ctx := context.Background()
	directory := t.TempDir()

	var store coreOwner

	var err error

	store, _ = factory(t, directory)

	calls := 0

	failures := 2
	if reopen {
		failures = 1
	}

	runtime := receiptRecoveryRuntime(t, store, failures, &calls)
	defer func() {
		requireNoError(t, runtime.Close(ctx), "close runtime: %v")
		requireNoError(t, store.Close(), "close store: %v")
	}()

	turn, err := m.NewTurn(m.Scope{Key: "test:receipt-recovery", Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"},
		"turn", time.Now().UTC(), "Я живу в Бишкеке", "OK")
	requireNoError(t, err, "turn: %v")

	request := m.SequencedTurn{ProducerID: "recovery-producer", ScopeSequence: 1, Fingerprint: "delivery", Turn: turn}
	for range failures {
		_, err := runtime.IngestSequencedTurn(ctx, request)
		if err == nil {
			t.Fatal("injected receipt failure returned success")
		}

		requireRecoveryState(t, store, request, calls, false)
	}

	if reopen {
		requireNoError(t, runtime.Close(ctx), "close before reopen: %v")
		requireNoError(t, store.Close(), "close before reopen: %v")

		store, _ = factory(t, directory)

		runtime = receiptRecoveryRuntime(t, store, 0, &calls)
	}

	completeReceiptRecovery(t, runtime, store, request, &calls)
}

func completeReceiptRecovery(t *testing.T, runtime *ordered.SequencedIngestRuntime,
	store coreOwner, request m.SequencedTurn, calls *int,
) {
	t.Helper()

	ctx := context.Background()
	changed := request
	changed.Turn = changedIdentityTurn(request.Turn)
	_, err := runtime.IngestSequencedTurn(ctx, changed)
	requirePermanentIdentityConflict(t, err)
	requireRecoveryState(t, store, request, *calls, false)
	outcome, err := runtime.IngestSequencedTurn(ctx, request)
	requireNoError(t, err, "recover receipt without provider: %v")
	requireRecoveredReceipt(t, outcome, m.IngestDispositionAccepted, 1)
	requireRecoveryState(t, store, request, *calls, true)
	duplicate, err := runtime.IngestSequencedTurn(ctx, request)
	requireNoError(t, err, "duplicate after recovery: %v")
	requireRecoveredReceipt(t, duplicate, m.IngestDispositionDuplicate, 1)

	request.ScopeSequence = 2
	request.Fingerprint = "next-delivery"
	next, err := runtime.IngestSequencedTurn(ctx, request)
	requireNoError(t, err, "next sequence canonical replay: %v")
	requireRecoveredReceipt(t, next, m.IngestDispositionAccepted, 2)
	requireRecoveryState(t, store, request, *calls, true)
}

func requireRecoveryState(t *testing.T, store coreOwner, request m.SequencedTurn, calls int, receipt bool) {
	t.Helper()

	state, err := store.LoadScopeState(context.Background(), request.Turn.Scope)
	requireNoError(t, err, "scope state: %v")

	if calls != 1 || state.ChangeSeq != 1 || state.Version != 1 {
		t.Fatalf("canonical mutation/model repeated: calls=%d state=%+v", calls, state)
	}

	cursor, found, err := store.LoadLatestIngestSequence(context.Background(), request.ProducerID, request.Turn.Scope)
	requireNoError(t, err, "receipt cursor: %v")

	if found != receipt || (receipt && cursor.Latest != request.ScopeSequence) {
		t.Fatalf("receipt accounting: found=%t cursor=%+v request=%d", found, cursor, request.ScopeSequence)
	}
}

func requireRecoveredReceipt(t *testing.T, outcome m.IngestOutcome, disposition m.IngestDisposition, sequence uint64) {
	t.Helper()

	if outcome.Disposition != disposition || outcome.ScopeSequence != sequence ||
		outcome.ScopeVersion != 1 || outcome.ScopeChangeSeq != 1 || len(outcome.OperationIDs) != 1 {
		t.Fatalf("recovered outcome=%+v", outcome)
	}
}
