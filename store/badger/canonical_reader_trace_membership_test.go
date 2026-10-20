package badger //nolint:testpackage // Measures actual storage reads and validates private source membership.

import (
	"bytes"
	"context"
	"reflect"
	"strconv"
	"testing"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestTraceMessageReadsStaySourceLocalWithGrowingHistory(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

	reads := 0
	reader.beforeTraceMessageRecordRead = func() error {
		reads++

		return nil
	}

	baseline, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err != nil {
		t.Fatal(err)
	}

	for _, count := range []int{1, 257} {
		addUnrelatedTraceMessages(t, store, mutation, count)

		for request := 0; request < 3; request++ {
			assertSourceLocalTrace(t, reader, mutation, baseline, &reads)
		}
	}

	key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, "unrelated-corrupt")

	if err := store.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, []byte("corrupt unrelated record"))
	}); err != nil {
		t.Fatal(err)
	}

	assertSourceLocalTrace(t, reader, mutation, baseline, &reads)
}

func addUnrelatedTraceMessages(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation, count int) {
	t.Helper()

	if err := store.db.Update(func(txn *badger.Txn) error {
		for i := 0; i < count; i++ {
			message := mutation.Messages[0]
			message.MessageID = "unrelated-" + strconv.Itoa(i)
			message.SourceID = "unrelated-source"

			key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
			if err := putCanonicalRecord(txn, key, badgerRecordMessage, message); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTraceRejectsUncertifiableMessageMembership(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "incomplete", "duplicate", "cross-source"} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if err := store.db.Update(func(txn *badger.Txn) error {
				return corruptTraceMembership(txn, mutation, kind)
			}); err != nil {
				t.Fatal(err)
			}

			reader, err := NewCanonicalReader(store)
			if err != nil {
				t.Fatal(err)
			}

			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
			if err == nil || len(response.Sources) != 0 || len(response.Revisions) != 0 {
				t.Fatalf("uncertifiable membership disclosed graph: %+v %v", response, err)
			}
		})
	}
}

func TestTracePreparedMembershipCompletenessIsBounded(t *testing.T) {
	for _, overflow := range []bool{false, true} {
		t.Run(strconv.FormatBool(overflow), func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if overflow {
				growTraceOverflowMembership(t, store, mutation)
			}

			makePrivacyHistoryOld(t, store, mutation)

			if err := store.PrepareSourcePrivacy(context.Background()); err != nil {
				t.Fatal(err)
			}

			var record struct {
				Complete bool `json:"message_membership_complete"`
			}

			key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, mutation.Sources[0].SourceID)

			if err := store.db.View(func(txn *badger.Txn) error {
				return getCanonicalRecord(txn, key, badgerRecordSourcePrivacy, &record)
			}); err != nil {
				t.Fatal(err)
			}

			if record.Complete == overflow {
				t.Fatalf("membership complete=%v, overflow=%v", record.Complete, overflow)
			}
		})
	}
}

func TestTraceMembershipPreservesStandardRawGraphAndOrdering(t *testing.T) {
	for _, kind := range []string{"uncertified", "certified", "certified-lineage", "migrated"} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			expectedTurn := canonicalReaderTestTurn(t, mutation.Scope, mutation.Operation.CommittedAt)
			configureTraceMembershipFixture(t, &mutation, kind, expectedTurn)

			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if kind == "uncertified" {
				reverseTraceMembership(t, store, mutation.Sources[0])
			}

			reader, err := NewCanonicalReader(store)
			if err != nil {
				t.Fatal(err)
			}

			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
			if err != nil {
				t.Fatal(err)
			}

			assertTraceRawGraph(t, response, expectedTurn)
		})
	}
}

func configureTraceMembershipFixture(t *testing.T, mutation *m.CanonicalMutation, kind string, turn m.Turn) {
	t.Helper()

	switch kind {
	case "certified", "certified-lineage":
		if kind == "certified-lineage" {
			turn.Conversation.LineageID = "lineage"
			turn.Conversation.PreviousConversationID = "previous"
		}

		fingerprint, err := m.CanonicalTurnFingerprint(turn)
		if err != nil {
			t.Fatal(err)
		}

		certificate := m.CanonicalTurnCertificate{Version: m.CanonicalTurnFingerprintVersion,
			SourceID: turn.SourceID, Fingerprint: fingerprint}
		for _, message := range turn.Messages {
			certificate.MessageIDs = append(certificate.MessageIDs, message.MessageID)
		}

		mutation.TurnIdentity = &m.CanonicalTurnIdentityCommit{Certificate: certificate, Turn: turn}
	case "migrated":
		embedPrivacyHistory(t, mutation)
	}
}

func TestTraceMessagePayloadBoundsReturnEmptyGraph(t *testing.T) {
	for _, kind := range []string{
		"oversized-message", "aggregate-messages", "oversized-source",
		"oversized-envelope", "missing-member", "member-identity",
	} {
		t.Run(kind, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if err := store.db.Update(func(txn *badger.Txn) error {
				return corruptTraceMessagePayload(txn, mutation, kind)
			}); err != nil {
				t.Fatal(err)
			}

			reader, err := NewCanonicalReader(store)
			if err != nil {
				t.Fatal(err)
			}

			response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
			assertEmptyTracePayloadBound(t, response, err, kind)
		})
	}
}

func corruptTraceMessagePayload(txn *badger.Txn, mutation m.CanonicalMutation, kind string) error {
	message := mutation.Messages[0]
	key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)

	switch kind {
	case "oversized-source":
		return oversizeBoundTraceSource(txn, mutation)
	case "missing-member":
		return txn.Delete(key)
	case "member-identity":
		message.MessageID = "wrong-message-identity"
	case "oversized-envelope":
		return txn.Set(key, bytes.Repeat([]byte("x"), maxCanonicalValueBytes+1))
	case "oversized-message":
		data := bytes.Repeat([]byte("x"), m.MaxDerivedTurnTextBytes+1)

		message.Payload = payloadRef("oversized-message", data)
		if err := putCanonicalMutationPayloads(txn, []m.CanonicalPayload{{Ref: message.Payload, Data: data}}); err != nil {
			return err
		}
	case "aggregate-messages":
		return growTraceMessageAggregate(txn, mutation)
	}

	return putCanonicalRecord(txn, key, badgerRecordMessage, message)
}

func growTraceMessageAggregate(txn *badger.Txn, mutation m.CanonicalMutation) error {
	data := bytes.Repeat([]byte("x"), m.MaxDerivedTurnTextBytes/2+1)
	for i, message := range mutation.Messages {
		message.Payload = payloadRef("aggregate-"+strconv.Itoa(i), data)
		if err := putCanonicalMutationPayloads(txn, []m.CanonicalPayload{{Ref: message.Payload, Data: data}}); err != nil {
			return err
		}

		key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
		if err := putCanonicalRecord(txn, key, badgerRecordMessage, message); err != nil {
			return err
		}
	}

	return nil
}

func TestTraceMessageExtensionInvalidatesCompleteCapture(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	extension := m.CanonicalMutation{SchemaVersion: m.CanonicalSchemaVersionV1,
		Scope: mutation.Scope, ExpectedScopeVersion: 1,
		Operation: m.OperationRecord{OperationID: "extend-membership", Fingerprint: "extend-membership",
			CommittedAt: mutation.Operation.CommittedAt},
		Messages: append([]m.MessageRecord(nil), mutation.Messages[:1]...)}

	extension.Messages[0].MessageID = "unclassified-extra-message"
	if _, err := store.ApplyCanonicalMutation(context.Background(), extension); err != nil {
		t.Fatal(err)
	}

	reader, err := NewCanonicalReader(store)
	if err != nil {
		t.Fatal(err)
	}

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err == nil || len(response.Sources) != 0 || len(response.Revisions) != 0 {
		t.Fatalf("stale membership capture disclosed graph: %+v %v", response, err)
	}
}

func assertSourceLocalTrace(t *testing.T, reader *CanonicalReader, mutation m.CanonicalMutation,
	baseline m.TraceResponse, reads *int) {
	t.Helper()

	*reads = 0

	response, err := reader.Trace(context.Background(), privacyTraceRequest(mutation))
	if err != nil {
		t.Fatal(err)
	}

	if *reads != 2 {
		t.Fatalf("trace read %d messages; want2", *reads)
	}

	if !reflect.DeepEqual(response, baseline) {
		t.Fatal("unrelated history changed raw trace graph")
	}
}

func corruptTraceMembership(txn *badger.Txn, mutation m.CanonicalMutation, kind string) error {
	source := mutation.Sources[0]

	key, _ := canonicalKey(mutation.Scope, badgerRecordSourcePrivacy, source.SourceID)
	if kind == "missing" {
		return txn.Delete(key)
	}

	record, _, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	switch kind {
	case "empty":
		record.MessageIDs = nil
	case "incomplete":
		return putCanonicalRecord(txn, key, badgerRecordSourcePrivacy, map[string]any{
			"version": 1, "coverage_version": 1, "scope": source.Scope, "source_id": source.SourceID,
			"payload": source.Payload, "state": "standard", "message_ids": record.MessageIDs,
			"message_membership_complete": false})
	case "duplicate":
		record.MessageIDs = append(record.MessageIDs, record.MessageIDs[0])
	case "cross-source":
		message := mutation.Messages[0]
		message.MessageID = "foreign-message"
		message.SourceID = "foreign-source"

		memberKey, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
		if err := putCanonicalRecord(txn, memberKey, badgerRecordMessage, message); err != nil {
			return err
		}

		record.MessageIDs = []string{message.MessageID, mutation.Messages[1].MessageID}
	}

	return writeSourcePrivacy(txn, record)
}

func reverseTraceMembership(t *testing.T, store *CanonicalStore, source m.CanonicalSourceRecord) {
	t.Helper()

	if err := store.db.Update(func(txn *badger.Txn) error {
		record, _, err := loadSourcePrivacy(txn, source)
		if err != nil {
			return err
		}

		record.MessageIDs[0], record.MessageIDs[1] = record.MessageIDs[1], record.MessageIDs[0]

		return writeSourcePrivacy(txn, record)
	}); err != nil {
		t.Fatal(err)
	}
}

func assertTraceRawGraph(t *testing.T, response m.TraceResponse, expectedTurn m.Turn) {
	t.Helper()

	if err := response.Validate(10); err != nil {
		t.Fatal(err)
	}

	if len(response.Sources) != 1 || !reflect.DeepEqual(response.Sources[0], privacyEmbeddedRecord(expectedTurn)) {
		t.Fatalf("raw graph shape/content/order changed: %+v", response.Sources)
	}

	if len(response.Revisions) != 1 || response.Revisions[0].Canonical == nil ||
		response.Revisions[0].Canonical.Text != "protected memory" {
		t.Fatalf("revision graph changed: %+v", response.Revisions)
	}
}

func growTraceOverflowMembership(t *testing.T, store *CanonicalStore, mutation m.CanonicalMutation) {
	t.Helper()

	if err := store.db.Update(func(txn *badger.Txn) error {
		for i := 0; i < maxCanonicalTraceSourceMessages; i++ {
			message := mutation.Messages[0]
			message.MessageID = "extra-" + strconv.Itoa(i)
			message.Role = m.MessageRoleTool

			key, _ := canonicalKey(mutation.Scope, badgerRecordMessage, message.MessageID)
			if err := putCanonicalRecord(txn, key, badgerRecordMessage, message); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func assertEmptyTracePayloadBound(t *testing.T, response m.TraceResponse, err error, kind string) {
	t.Helper()

	if err == nil || len(response.Sources) != 0 || len(response.Revisions) != 0 {
		t.Fatalf("unsafe message context returned: %+v %v", response, err)
	}

	if kind == "oversized-message" || kind == "aggregate-messages" || kind == "oversized-source" {
		if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeLimitExceeded {
			t.Fatalf("payload bound did not reject before loading: %v", err)
		}
	}
}

func oversizeBoundTraceSource(txn *badger.Txn, mutation m.CanonicalMutation) error {
	source := mutation.Sources[0]

	record, _, err := loadSourcePrivacy(txn, source)
	if err != nil {
		return err
	}

	source.Payload = m.PayloadRef{ID: "oversized-source",
		Digest: source.Payload.Digest, ByteSize: maxCanonicalValueBytes + 1}

	record.Payload = source.Payload
	if err := writeSourcePrivacy(txn, record); err != nil {
		return err
	}

	key, _ := canonicalKey(mutation.Scope, badgerRecordSource, source.SourceID)

	return putCanonicalRecord(txn, key, badgerRecordSource, source)
}
