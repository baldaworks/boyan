package badger //nolint:testpackage // Fault injection exercises durable raw cursor recovery and blob ownership.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestScopeEraseRunnerResumesPayloadScrubAndCommitsOnce(t *testing.T) {
	dir := t.TempDir()

	store, err := OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	scope := memory.Scope{Key: "telegram:runner", Kind: memory.ScopeKindPersonal}
	refs := seedEraseHistory(t, store, scope, 260)

	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
	_, err = store.BeginScopeErase(context.Background(), request)
	requireEraseTestNoError(t, err)

	advanceEraseTestToStage(t, store, scope, "erase", memory.ScopeEraseScrubMessages)

	requireEraseTestNoError(t, store.Close())

	store, err = OpenCanonicalStore(dir)
	requireEraseTestNoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	pending, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}

	journal, err := store.RunScopeErase(context.Background(), request)
	requireEraseTestNoError(t, err)

	if journal.Stage != memory.ScopeEraseComplete || journal.SourceCount != 260 || journal.Outcome == nil {
		t.Fatalf("journal=%+v", journal)
	}

	requireErasePayloadsAbsent(t, store, refs)

	replay, err := store.RunScopeErase(context.Background(), request)
	if err != nil || replay.Outcome.ScopeVersion != journal.Outcome.ScopeVersion {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}

	state, err := store.LoadScopeState(context.Background(), scope)
	if err != nil || state.ChangeSeq != 1 {
		t.Fatalf("state=%+v err=%v", state, err)
	}
}

func seedEraseHistory(t *testing.T, store *CanonicalStore, scope memory.Scope, count int) []memory.PayloadRef {
	t.Helper()

	refs := make([]memory.PayloadRef, 0, count*3)
	err := store.db.Update(func(txn *badger.Txn) error {
		for i := range count {
			next, err := seedEraseTurn(txn, scope, i)
			if err != nil {
				return err
			}

			refs = append(refs, next...)
		}

		return nil
	})
	requireEraseTestNoError(t, err)

	return refs
}

func seedEraseTurn(txn *badger.Txn, scope memory.Scope, index int) ([]memory.PayloadRef, error) {
	export := fmt.Sprintf("export-%04d", index)
	sourceID := memory.TurnSourceID(export)
	data := []byte(fmt.Sprintf(`{"export_id":%q,"conversation_id":"conversation","source_turn_id":%q}`, export, export))
	sourceRef := eraseTestPayload(fmt.Sprintf("source-%s-%s-%04d", scope.Key, scope.Kind, index), data)
	messageRef := eraseTestPayload(fmt.Sprintf("message-%s-%s-%04d", scope.Key, scope.Kind, index), []byte("private text"))

	source := memory.CanonicalSourceRecord{SourceID: sourceID, Scope: scope,
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: sourceRef}
	if err := putBadgerCanonicalRecord(txn, scope, badgerRecordSource, sourceID, source); err != nil {
		return nil, err
	}

	message := memory.MessageRecord{MessageID: fmt.Sprintf("message-%04d", index),
		SourceID: sourceID, Role: memory.MessageRoleUser, Payload: messageRef}
	if err := putBadgerCanonicalRecord(txn, scope, badgerRecordMessage, message.MessageID, message); err != nil {
		return nil, err
	}

	payloads := []memory.CanonicalPayload{{Ref: sourceRef, Data: data}, {Ref: messageRef, Data: []byte("private text")}}
	if err := putCanonicalMutationPayloads(txn, payloads); err != nil {
		return nil, err
	}

	revisionRef, err := seedEraseRevision(txn, scope, index, sourceID, message.MessageID)

	return []memory.PayloadRef{sourceRef, messageRef, revisionRef}, err
}

func seedEraseRevision(
	txn *badger.Txn, scope memory.Scope, index int, sourceID, messageID string,
) (memory.PayloadRef, error) {
	ref := eraseTestPayload(fmt.Sprintf("revision-%s-%s-%04d", scope.Key, scope.Kind, index), []byte("private statement"))

	revision := memory.MemoryRevision{
		SchemaVersion: memory.MemorySchemaVersion, RevisionID: fmt.Sprintf("revision-%04d", index),
		ItemID: "item", Revision: uint64(index + 1), Temporal: memory.Temporal{ObservedAt: time.Now()},
		Evidence: []memory.EvidenceRef{{SourceID: sourceID, MessageID: messageID, Role: memory.MessageRoleUser,
			EndByte: 4, AssertionMode: memory.AssertionModeUser}},
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: ref,
	}
	if err := putBadgerCanonicalRecord(txn, scope, badgerRecordRevision, revision.RevisionID, revision); err != nil {
		return memory.PayloadRef{}, err
	}

	err := putCanonicalMutationPayloads(txn, []memory.CanonicalPayload{{Ref: ref, Data: []byte("private statement")}})

	return ref, err
}

func eraseTestPayload(id string, data []byte) memory.PayloadRef {
	digest := sha256.Sum256(data)

	return memory.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}

func TestScopeErasePayloadBatchRollbackKeepsCursorAndRetryableProgress(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:rollback", Kind: memory.ScopeKindPersonal}
	refs := seedEraseHistory(t, store, scope, 1)

	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
	_, err = store.BeginScopeErase(context.Background(), request)
	requireEraseTestNoError(t, err)

	advanceEraseTestToStage(t, store, scope, "erase", memory.ScopeEraseScrubSources)

	store.beforeScopeEraseBatchCommit = func(memory.ScopeEraseJournal) error { return context.Canceled }
	if _, err := store.AdvanceScopeErase(context.Background(), scope, "erase"); err == nil {
		t.Fatal("faulted scrub succeeded")
	}

	if _, err := store.LoadPayload(context.Background(), refs[0]); err != nil {
		t.Fatalf("rolled back payload delete leaked: %v", err)
	}

	journal, found, err := store.LoadScopeErase(context.Background(), scope, "erase")
	if err != nil || !found || journal.Stage != memory.ScopeEraseScrubSources || len(journal.Cursor) != 0 {
		t.Fatalf("journal=%+v found=%t err=%v", journal, found, err)
	}

	store.beforeScopeEraseBatchCommit = nil
	_, err = store.RunScopeErase(context.Background(), request)
	requireEraseTestNoError(t, err)
}

func requireEraseTestNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func advanceEraseTestToStage(
	t *testing.T, store *CanonicalStore, scope memory.Scope, operationID string, stage memory.ScopeEraseStage,
) {
	t.Helper()

	for range 10000 {
		journal, err := store.AdvanceScopeErase(context.Background(), scope, operationID)
		requireEraseTestNoError(t, err)

		if journal.Stage == stage {
			return
		}
	}

	t.Fatalf("erase did not reach stage %s", stage)
}

func requireErasePayloadsAbsent(t *testing.T, store *CanonicalStore, refs []memory.PayloadRef) {
	t.Helper()

	for _, ref := range refs {
		if _, err := store.LoadPayload(context.Background(), ref); err == nil {
			t.Fatalf("payload %s survived", ref.ID)
		}
	}
}
