package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	forgetops "github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func nativeFlow(t *testing.T, factory coreFactory) (coreReader,
	*core.CanonicalTurnProcessor,
	m.Turn,
	m.CanonicalMutationOutcome,
	coreOwner) {
	t.Helper()

	var store coreOwner

	var reader coreReader

	var err error

	if factory != nil {
		store, reader = factory(t, t.TempDir())
	} else {
		legacy, openErr := b.Open(t.TempDir())
		requireNoError(t, openErr, "open: %v")

		store = legacy
		reader, err = b.NewCanonicalReader(legacy)
		requireNoError(t, err, "reader: %v")
	}

	t.Cleanup(func() { requireNoError(t, store.Close(), "close: %v") })

	processor,
		err := core.NewCanonicalTurnProcessor(store,
		canonicalExtractorFunc(extractTestStateCandidate),
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	turn,
		err := m.NewTurn(m.Scope{Key: "test:native",
		Kind: m.ScopeKindPersonal},
		m.ConversationRef{ConversationID: "conversation",
			AgentConversationID: "agent"},
		"turn-old",
		time.Now().UTC().Add(-24*time.Hour),
		"Я живу в Бишкеке",
		"OK")
	requireNoError(t, err, "turn: %v")
	outcome, err := processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "process: %v")

	return reader, processor, turn, outcome, store
}

func TestNativeTraceRoundTrip(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, _, turn, outcome, _ := nativeFlow(t, factory)
		records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, outcome.RevisionIDs)
		requireNoError(t, err, "records: %v")

		if len(records) != 1 {
			t.Fatalf("records: %d", len(records))
		}

		response,
			err := reader.Trace(context.Background(),
			m.TraceRequest{Scope: turn.Scope,
				Root: m.RevisionRef{ItemID: records[0].ItemID,
					RevisionID: records[0].RevisionID},
				MaxNodes: 10})
		requireNoError(t, err, "trace native searchable revision: %v")
		requireNoError(t, response.Validate(10), "response: %v")

		if len(response.Sources) != 1 || response.Sources[0].Turn.Messages[0].Text != turn.Messages[0].Text {
			t.Fatalf("source: %+v", response.Sources)
		}
	})
}

func TestNativeTraceSupersessionAndIsolation(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, processor, turn, _, _ := nativeFlow(t, factory)
		next,
			err := m.NewTurn(turn.Scope,
			turn.Conversation,
			"turn-new",
			turn.CompletedAt.Add(time.Hour),
			"Я живу в Оше",
			"OK")
		requireNoError(t, err, "next turn: %v")
		outcome, err := processor.ProcessTurn(context.Background(), next, testProcessorDerivation())
		requireNoError(t, err, "update: %v")
		records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, outcome.RevisionIDs)
		requireNoError(t, err, "records: %v")

		if len(records) != 1 {
			t.Fatalf("records: %d", len(records))
		}

		request := m.TraceRequest{Scope: turn.Scope,
			Root: m.RevisionRef{ItemID: records[0].ItemID,
				RevisionID: records[0].RevisionID},
			MaxNodes: 10}
		response, err := reader.Trace(context.Background(), request)
		requireNoError(t, err, "trace: %v")

		if len(response.Revisions) != 2 ||
			response.Revisions[0].Canonical == nil ||
			response.Revisions[0].Canonical.Supersedes == nil {
			t.Fatalf("supersession missing: %+v", response.Revisions)
		}

		requireNoError(t, response.Validate(10), "response: %v")

		request.Scope.Key = "test:other"
		if _, err = reader.Trace(context.Background(), request); err == nil {
			t.Fatal("foreign scope trace succeeded")
		}
	})
}

func TestNativeTraceForgottenSource(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, _, turn, outcome, store := nativeFlow(t, factory)
		records, err := reader.LoadRecallRecords(context.Background(), turn.Scope, outcome.RevisionIDs)
		requireNoError(t, err, "records: %v")

		if len(records) != 1 {
			t.Fatalf("records: %d", len(records))
		}

		adapter, err := forgetops.NewCanonicalForgetAdapter(store)
		requireNoError(t, err, "forget adapter: %v")
		_,
			err = adapter.ForgetSource(context.Background(),
			m.ForgetSourceCommand{SchemaVersion: m.DerivedSchemaVersionV1,
				Source: m.SourceRef{Scope: turn.Scope,
					ExportID:       turn.ExportID,
					ConversationID: turn.Conversation.ConversationID,
					SourceTurnID:   turn.SourceTurnID},
				ForgottenAt: time.Now().UTC()})
		requireNoError(t, err, "forget: %v")

		_,
			err = reader.Trace(context.Background(),
			m.TraceRequest{Scope: turn.Scope,
				Root: m.RevisionRef{ItemID: records[0].ItemID,
					RevisionID: records[0].RevisionID},
				MaxNodes: 10})
		if err == nil {
			t.Fatal("forgotten content traced")
		}
	})
}
