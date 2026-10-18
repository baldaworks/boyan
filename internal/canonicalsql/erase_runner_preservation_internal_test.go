package canonicalsql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func seedEraseHistory(t *testing.T, store *Owner, scope memory.Scope, count int) []memory.PayloadRef {
	t.Helper()

	refs := make([]memory.PayloadRef, 0, count*3)
	err := store.Update(t.Context(), func(ctx context.Context, txn *Tx) error {
		for i := range count {
			next, err := seedEraseTurn(ctx, txn, scope, i)
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

func seedEraseTurn(ctx context.Context, txn *Tx, scope memory.Scope, index int) ([]memory.PayloadRef, error) {
	export := fmt.Sprintf("export-%04d", index)
	sourceID := memory.TurnSourceID(export)
	data := []byte(fmt.Sprintf(`{"export_id":%q,"conversation_id":"conversation","source_turn_id":%q}`, export, export))
	sourceRef := eraseTestPayload(fmt.Sprintf("source-%s-%s-%04d", scope.Key, scope.Kind, index), data)
	messageRef := eraseTestPayload(fmt.Sprintf("message-%s-%s-%04d", scope.Key, scope.Kind, index), []byte("private text"))

	source := memory.CanonicalSourceRecord{SourceID: sourceID, Scope: scope,
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: sourceRef}
	if err := putEraseFixtureRecord(ctx, txn, scope, Source, sourceID, source); err != nil {
		return nil, err
	}

	message := memory.MessageRecord{MessageID: fmt.Sprintf("message-%04d", index),
		SourceID: sourceID, Role: memory.MessageRoleUser, Payload: messageRef}
	if err := putEraseFixtureRecord(ctx, txn, scope, Message, message.MessageID, message); err != nil {
		return nil, err
	}

	payloads := []memory.CanonicalPayload{{Ref: sourceRef, Data: data}, {Ref: messageRef, Data: []byte("private text")}}
	if err := putCanonicalMutationPayloads(ctx, txn, payloads); err != nil {
		return nil, err
	}

	revisionRef, err := seedEraseRevision(ctx, txn, scope, index, sourceID, message.MessageID)

	return []memory.PayloadRef{sourceRef, messageRef, revisionRef}, err
}

func seedEraseRevision(
	ctx context.Context, txn *Tx, scope memory.Scope, index int, sourceID, messageID string,
) (memory.PayloadRef, error) {
	ref := eraseTestPayload(fmt.Sprintf("revision-%s-%s-%04d", scope.Key, scope.Kind, index), []byte("private statement"))

	revision := memory.MemoryRevision{
		SchemaVersion: memory.MemorySchemaVersion, RevisionID: fmt.Sprintf("revision-%04d", index),
		ItemID: "item", Revision: uint64(index + 1), Temporal: memory.Temporal{ObservedAt: time.Now()},
		Evidence: []memory.EvidenceRef{{SourceID: sourceID, MessageID: messageID, Role: memory.MessageRoleUser,
			EndByte: 4, AssertionMode: memory.AssertionModeUser}},
		Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: ref,
	}
	if err := putEraseFixtureRecord(ctx, txn, scope, Revision, revision.RevisionID, revision); err != nil {
		return memory.PayloadRef{}, err
	}

	err := putCanonicalMutationPayloads(ctx, txn, []memory.CanonicalPayload{{Ref: ref, Data: []byte("private statement")}})

	return ref, err
}

func eraseTestPayload(id string, data []byte) memory.PayloadRef {
	digest := sha256.Sum256(data)

	return memory.PayloadRef{ID: id, Digest: hex.EncodeToString(digest[:]), ByteSize: uint32(len(data))}
}

func requireEraseTestNoError(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}

func advanceEraseTestToStage(
	t *testing.T, store *Owner, scope memory.Scope, operationID string, stage memory.ScopeEraseStage,
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

func requireErasePayloadsAbsent(t *testing.T, store *Owner, refs []memory.PayloadRef) {
	t.Helper()

	for _, ref := range refs {
		if _, err := store.LoadPayload(context.Background(), ref); err == nil {
			t.Fatalf("payload %s survived", ref.ID)
		}
	}
}
