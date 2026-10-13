package canonicalcore_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
	db "github.com/dgraph-io/badger/v4"
)

func TestScopeForgetResumesLegacyCrashWindows(t *testing.T) {
	t.Parallel()

	for _, committed := range []bool{false, true} {
		name := "after-denial"
		if committed {
			name = "after-outcome"
		}

		t.Run(name, func(t *testing.T) { testLegacyForgetCrash(t, committed) })
	}
}

func testLegacyForgetCrash(t *testing.T, committed bool) {
	t.Helper()
	_, _, turn, outcome, store := nativeFlow(t)
	ctx := context.Background()
	refs := nativeFixturePayloadRefs(t, store)
	revisions, err := store.LoadCanonicalRevisions(ctx, m.CanonicalRevisionReadRequest{
		Scope: turn.Scope, RevisionIDs: outcome.RevisionIDs,
	})
	requireNoError(t, err, "load revisions: %v")

	if len(revisions) != 1 {
		t.Fatalf("revisions=%d", len(revisions))
	}

	at := time.Now().UTC()
	requireNoError(t, store.DenySource(ctx, turn.Scope, m.TurnSourceID(turn.ExportID), at), "deny source: %v")
	requireNoError(t, store.DenyRevision(ctx, turn.Scope, revisions[0].RevisionID, at), "deny revision: %v")

	command := m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Scope: turn.Scope, RequestID: "legacy-crash", ForgottenAt: at}
	if committed {
		commitLegacyScopeForget(t, store, turn, revisions[0], command)
	}

	adapter, err := forgetops.NewCanonicalForgetAdapter(store)
	requireNoError(t, err, "adapter: %v")
	result, err := adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "recover forget: %v")
	requireNativePayloadsAbsent(t, store, refs)

	replay, err := adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "replay forget: %v")

	if replay.ScopeVersion != result.ScopeVersion {
		t.Fatalf("replayed version=%d want%d", replay.ScopeVersion, result.ScopeVersion)
	}
}

func commitLegacyScopeForget(t *testing.T, store *b.CanonicalStore, turn m.Turn,
	revision m.MemoryRevision, command m.ForgetScopeCommand,
) {
	t.Helper()

	ctx := context.Background()
	state, err := store.LoadScopeState(ctx, turn.Scope)
	requireNoError(t, err, "scope state: %v")
	operationID, err := m.CanonicalForgetOperationID(m.ForgetKindScope, turn.Scope, m.SourceRef{}, command.RequestID)
	requireNoError(t, err, "operation ID: %v")
	_, err = store.CommitCanonicalForget(ctx, m.CanonicalForgetCommitRequest{
		Scope: turn.Scope, Kind: m.ForgetKindScope, OperationID: operationID,
		Fingerprint: m.CanonicalForgetFingerprint(
			operationID, turn.Scope, m.ForgetKindScope, m.SourceRef{}, command.RequestID,
		),
		ExpectedScopeVersion: state.Version, ForgottenAt: command.ForgottenAt,
		Sources: []m.SourceRef{{Scope: turn.Scope, ExportID: turn.ExportID,
			ConversationID: turn.Conversation.ConversationID, SourceTurnID: turn.SourceTurnID}},
		Revisions: []m.RevisionRef{{ItemID: revision.ItemID, RevisionID: revision.RevisionID}},
	})
	requireNoError(t, err, "legacy outcome commit: %v")
}

func nativeFixturePayloadRefs(t *testing.T, store *b.CanonicalStore) []m.PayloadRef {
	t.Helper()

	var refs []m.PayloadRef

	err := store.DB().View(func(txn *db.Txn) error {
		iterator := txn.NewIterator(db.DefaultIteratorOptions)
		defer iterator.Close()

		for iterator.Rewind(); iterator.Valid(); iterator.Next() {
			encoded, err := iterator.Item().ValueCopy(nil)
			if err != nil {
				return err
			}

			envelope, err := m.UnmarshalRecordEnvelope(encoded)
			if err != nil {
				return err
			}

			switch envelope.RecordType {
			case "source", "message", "revision":
				var record struct {
					Payload m.PayloadRef `json:"payload"`
				}
				if err := json.Unmarshal(envelope.Payload, &record); err != nil {
					return err
				}

				refs = append(refs, record.Payload)
			}
		}

		return nil
	})
	requireNoError(t, err, "fixture payload refs: %v")

	if len(refs) == 0 {
		t.Fatal("fixture has no payload refs")
	}

	return refs
}

func requireNativePayloadsAbsent(t *testing.T, store *b.CanonicalStore, refs []m.PayloadRef) {
	t.Helper()

	for _, ref := range refs {
		if _, err := store.LoadPayload(context.Background(), ref); err == nil {
			t.Fatalf("forget returned success with retained payload %s", ref.ID)
		}
	}
}

func TestCompletedScopeForgetReplayPreservesLaterTurn(t *testing.T) {
	t.Parallel()
	reader, processor, turn, _, store := nativeFlow(t)
	ctx := context.Background()
	adapter, err := forgetops.NewCanonicalForgetAdapter(store)
	requireNoError(t, err, "adapter: %v")

	command := m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1,
		Scope: turn.Scope, RequestID: "completed-forget", ForgottenAt: time.Now().UTC()}
	_, err = adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "first forget: %v")
	later, err := m.NewTurn(turn.Scope, turn.Conversation, "turn-after-forget",
		turn.CompletedAt.Add(time.Hour), "Я живу в Оше", "OK")
	requireNoError(t, err, "later turn: %v")
	outcome, err := processor.ProcessTurn(ctx, later, testProcessorDerivation())
	requireNoError(t, err, "later process: %v")
	_, err = adapter.ForgetScope(ctx, command)
	requireNoError(t, err, "old request replay: %v")
	records, err := reader.LoadRecallRecords(ctx, later.Scope, outcome.RevisionIDs)
	requireNoError(t, err, "later recall: %v")

	if len(records) != 1 {
		t.Fatalf("later records lost: %d", len(records))
	}

	traced, err := reader.Trace(ctx, m.TraceRequest{Scope: later.Scope,
		Root: m.RevisionRef{ItemID: records[0].ItemID, RevisionID: records[0].RevisionID}, MaxNodes: 10})
	requireNoError(t, err, "later trace: %v")

	if len(traced.Sources) != 1 || traced.Sources[0].Turn.Messages[0].Text != later.Messages[0].Text {
		t.Fatalf("later source lost: %+v", traced.Sources)
	}
}
