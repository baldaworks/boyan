package badger //nolint:testpackage // Exercises serialized legacy evidence and certificate-backed forgotten replay.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestLegacyTurnIdentityMissingForgottenAndCorruptEvidence(t *testing.T) {
	tests := []struct {
		name            string
		denied, corrupt bool
		want            memory.ErrorCode
	}{
		{name: "missing", want: memory.CodeConflict},
		{name: "forgotten", denied: true, want: memory.CodeConflict},
		{name: "corrupt", corrupt: true, want: memory.CodeStoreFailure},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := OpenCanonicalStore(t.TempDir())
			requireEraseTestNoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			turn, source, _ := seedPreCertificateTurn(t, store)
			if test.denied {
				requireEraseTestNoError(t, store.DenySource(context.Background(), turn.Scope, turn.SourceID, time.Now()))
			}

			requireEraseTestNoError(t, store.DeletePayload(context.Background(), source.Payload))

			if test.corrupt {
				err = store.db.Update(func(txn *badger.Txn) error {
					return putSerializedLegacyPayload(txn, source.Payload, []byte("corrupted evidence"))
				})
				requireEraseTestNoError(t, err)
			}

			_, err = store.VerifyCanonicalTurn(context.Background(), turn)
			requireTurnIdentityCode(t, err, test.want)
		})
	}
}

func TestLegacyTurnIdentityDefaultNormalizationAndToolOrder(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	tool := memory.Message{Role: memory.MessageRoleTool, ToolName: "lookup", ToolCallID: "call", Text: "old tool text"}
	turn, _, _ := seedPreCertificateTurn(t, store, tool)
	turn.SourceID = ""
	turn.TerminalStatus = ""
	turn.CompletedAt = turn.CompletedAt.UTC()
	turn.Messages[0].MessageID = ""
	turn.Messages[1].MessageID = ""
	_, err = store.VerifyCanonicalTurn(context.Background(), turn)
	requireEraseTestNoError(t, err)
	other, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = other.Close() })

	second := tool
	second.ToolCallID = "second-call"
	ambiguous, _, _ := seedPreCertificateTurn(t, other, tool, second)
	_, err = other.VerifyCanonicalTurn(context.Background(), ambiguous)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func requireTurnIdentityCode(t *testing.T, err error, code memory.ErrorCode) {
	t.Helper()

	var typed *memory.Error
	if !errors.As(err, &typed) || typed.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestCertifiedForgottenTurnAllowsOnlyCommittedReplay(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	turn, source, messages := seedPreCertificateTurn(t, store)
	mutation := identityMutationForTurn(t, turn, source, messages, "original", 0)
	outcome, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	requireEraseTestNoError(t, err)
	requireEraseTestNoError(t, store.DenySource(context.Background(), turn.Scope, turn.SourceID, time.Now()))
	requireEraseTestNoError(t, store.DeletePayload(context.Background(), source.Payload))
	proof, err := store.VerifyCanonicalTurn(context.Background(), turn)
	requireEraseTestNoError(t, err)

	if !proof.Found || !proof.Forgotten {
		t.Fatalf("proof=%+v", proof)
	}

	for _, change := range []struct{ name, lineage, previous string }{
		{name: "lineage", lineage: "different-lineage"},
		{name: "previous conversation", previous: "different-previous"},
	} {
		t.Run(change.name, func(t *testing.T) {
			changed := turn
			changed.Conversation.LineageID = change.lineage
			changed.Conversation.PreviousConversationID = change.previous
			_, err := store.VerifyCanonicalTurn(context.Background(), changed)
			requireTurnIdentityCode(t, err, memory.CodeConflict)
		})
	}

	replay, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	requireEraseTestNoError(t, err)

	if replay.ScopeVersion != outcome.ScopeVersion || replay.ChangeSeq != outcome.ChangeSeq {
		t.Fatalf("replay=%+v original=%+v", replay, outcome)
	}

	mutation.Operation.OperationID = "new-derivation"
	mutation.Operation.Fingerprint = "new-fingerprint"
	mutation.ExpectedScopeVersion = outcome.ScopeVersion
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func TestTurnIdentityTransactionCannotCommitAcrossConcurrentForget(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	turn, source, messages := seedPreCertificateTurn(t, store)
	mutation := identityMutationForTurn(t, turn, source, messages, "operation", 0)
	store.beforeCanonicalMutationCommit = func() error {
		return store.DenySource(context.Background(), turn.Scope, turn.SourceID, time.Now())
	}

	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if !memory.IsRetryable(err) {
		t.Fatalf("mutation crossed concurrent forget: %v", err)
	}

	store.beforeCanonicalMutationCommit = nil
	state, err := store.LoadScopeState(context.Background(), turn.Scope)
	requireEraseTestNoError(t, err)

	if state.ChangeSeq != 0 {
		t.Fatalf("failed transaction advanced state: %+v", state)
	}

	_, err = store.VerifyCanonicalTurn(context.Background(), turn)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func identityMutationForTurn(t *testing.T, turn memory.Turn, source memory.CanonicalSourceRecord,
	messages []memory.MessageRecord, operationID string, version uint64) memory.CanonicalMutation {
	t.Helper()

	fingerprint, err := memory.CanonicalTurnFingerprint(turn)
	requireEraseTestNoError(t, err)

	certificate := memory.CanonicalTurnCertificate{
		Version: memory.CanonicalTurnFingerprintVersion, SourceID: turn.SourceID, Fingerprint: fingerprint,
	}
	for _, message := range turn.Messages {
		certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
	}

	return memory.CanonicalMutation{
		SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: turn.Scope, ExpectedScopeVersion: version,
		Operation: memory.OperationRecord{
			OperationID: operationID,
			Fingerprint: "derivation-fingerprint",
			CommittedAt: turn.CompletedAt,
		},
		Sources: []memory.CanonicalSourceRecord{source}, Messages: messages,
		TurnIdentity: &memory.CanonicalTurnIdentityCommit{Certificate: certificate, Turn: turn},
	}
}

func TestLegacyRawTurnPreservesCertifiableToolOrder(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	first := memory.Message{Role: memory.MessageRoleTool, ToolName: "lookup", ToolCallID: "first", Text: "first result"}
	second := first
	second.ToolCallID = "second"
	second.Text = "second result"
	turn, source, _ := seedPreCertificateTurn(t, store, first, second)
	archived := memory.SourceRecord{
		SchemaVersion: memory.DerivedSchemaVersionV1, State: memory.SourceStateActive, Turn: &turn,
		Ref: memory.SourceRef{
			Scope:          turn.Scope,
			ExportID:       turn.ExportID,
			ConversationID: turn.Conversation.ConversationID,
			SourceTurnID:   turn.SourceTurnID,
		},
	}
	data, err := json.Marshal(archived)
	requireEraseTestNoError(t, err)

	source.Payload = eraseTestPayload("legacy-raw-turn", data)
	err = store.db.Update(func(txn *badger.Txn) error {
		if err := putSerializedLegacyPayload(txn, source.Payload, data); err != nil {
			return err
		}

		return putSerializedLegacyRecord(txn, turn.Scope, badgerRecordSource, source.SourceID, source)
	})
	requireEraseTestNoError(t, err)
	_, err = store.VerifyCanonicalTurn(context.Background(), turn)
	requireEraseTestNoError(t, err)

	turn.Messages[2], turn.Messages[3] = turn.Messages[3], turn.Messages[2]
	_, err = store.VerifyCanonicalTurn(context.Background(), turn)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func TestCommittedTurnWithoutSourceCannotAcquireOrphanCertificate(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	turn, source, messages := seedPreCertificateTurn(t, store)

	mutation := identityMutationForTurn(t, turn, source, messages, "committed", 0)
	for _, ref := range append([]memory.PayloadRef{source.Payload}, messages[0].Payload, messages[1].Payload) {
		data, err := store.LoadPayload(context.Background(), ref)
		requireEraseTestNoError(t, err)

		mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: ref, Data: data})
	}

	err = store.db.Update(func(txn *badger.Txn) error {
		operationKey, err := badgerOperationKey(turn.Scope, "committed")
		if err != nil {
			return err
		}

		if err := putCanonicalRecord(txn, operationKey, badgerRecordOperation, badgerCanonicalOperation{
			Fingerprint: mutation.Operation.Fingerprint, Outcome: memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1},
		}); err != nil {
			return err
		}
		changeKey, err := badgerScopeChangeKey(turn.Scope, 1)
		if err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, changeKey, badgerRecordChange, memory.ScopeChange{
			Sequence: 1, ScopeVersion: 1, OperationID: "committed", OccurredAt: mutation.Operation.CommittedAt,
		}); err != nil {
			return err
		}
		scopeKey, err := badgerScopeKey(turn.Scope)
		if err != nil {
			return err
		}
		if err := putCanonicalRecord(txn, scopeKey, badgerRecordScope, memory.ScopeState{
			SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: turn.Scope, Version: 1, ChangeSeq: 1,
		}); err != nil {
			return err
		}

		sourceKey, err := canonicalKey(turn.Scope, badgerRecordSource, turn.SourceID)
		if err != nil {
			return err
		}

		return txn.Delete(sourceKey)
	})
	requireEraseTestNoError(t, err)
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func TestCertifiedRawOperationReplayRejectsChangedStructuralMessage(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	turn, source, messages := seedPreCertificateTurn(t, store)
	mutation := identityMutationForTurn(t, turn, source, messages, "certified-raw", 0)
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireEraseTestNoError(t, err)

	mutation.TurnIdentity = nil
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireEraseTestNoError(t, err)

	mutation.Messages = append([]memory.MessageRecord(nil), mutation.Messages...)
	mutation.Messages[0].Role = memory.MessageRoleAssistant
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	requireTurnIdentityCode(t, err, memory.CodeConflict)
}

func TestKnownLegacyExportKeyCannotBeMistakenForNewSource(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprintf("denied=%t", denied), func(t *testing.T) {
			store, err := OpenCanonicalStore(t.TempDir())
			requireEraseTestNoError(t, err)
			t.Cleanup(func() { _ = store.Close() })
			turn, source, messages := seedPreCertificateTurn(t, store)
			mutation := identityMutationForTurn(t, turn, source, messages, "new-derivation", 0)
			addIdentityFixturePayloads(t, store, &mutation, source, messages)
			seedExportKeyedLegacySource(t, store, turn, source, denied)
			_, err = store.VerifyCanonicalTurn(context.Background(), turn)
			requireTurnIdentityCode(t, err, memory.CodeConflict)
			_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
			requireTurnIdentityCode(t, err, memory.CodeConflict)
			state, err := store.LoadScopeState(context.Background(), turn.Scope)
			requireEraseTestNoError(t, err)

			if state.ChangeSeq != 0 {
				t.Fatalf("legacy fallback mutated state: %+v", state)
			}

			if denied {
				err = store.db.Update(func(txn *badger.Txn) error {
					key, err := canonicalKey(turn.Scope, badgerRecordSource, turn.ExportID)
					if err != nil {
						return err
					}

					return txn.Delete(key)
				})
				requireEraseTestNoError(t, err)
				_, err = store.VerifyCanonicalTurn(context.Background(), turn)
				requireTurnIdentityCode(t, err, memory.CodeConflict)
				_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
				requireTurnIdentityCode(t, err, memory.CodeConflict)
			}
		})
	}
}

func seedExportKeyedLegacySource(t *testing.T, store *CanonicalStore, turn memory.Turn,
	source memory.CanonicalSourceRecord, denied bool) {
	t.Helper()

	err := store.db.Update(func(txn *badger.Txn) error {
		nativeKey, err := canonicalKey(turn.Scope, badgerRecordSource, turn.SourceID)
		if err != nil {
			return err
		}

		if err := txn.Delete(nativeKey); err != nil {
			return err
		}

		source.SourceID = turn.ExportID

		return putSerializedLegacyRecord(txn, turn.Scope, badgerRecordSource, turn.ExportID, source)
	})
	requireEraseTestNoError(t, err)

	if denied {
		requireEraseTestNoError(t, store.DenySource(context.Background(), turn.Scope, turn.ExportID, time.Now()))
	}
}

func addIdentityFixturePayloads(t *testing.T, store *CanonicalStore, mutation *memory.CanonicalMutation,
	source memory.CanonicalSourceRecord, messages []memory.MessageRecord) {
	t.Helper()

	refs := []memory.PayloadRef{source.Payload}
	for _, message := range messages {
		refs = append(refs, message.Payload)
	}

	for _, ref := range refs {
		data, err := store.LoadPayload(context.Background(), ref)
		requireEraseTestNoError(t, err)

		mutation.Payloads = append(mutation.Payloads, memory.CanonicalPayload{Ref: ref, Data: data})
	}
}
