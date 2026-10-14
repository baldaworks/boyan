package badger //nolint:testpackage // Tests transactional private source authority and readiness.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func privacyFixture(t *testing.T) (*CanonicalStore, m.CanonicalMutation) {
	t.Helper()

	store, err := OpenCanonicalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	scope := m.Scope{Key: "test:privacy-writer", Kind: m.ScopeKindPersonal}
	turn := canonicalReaderTestTurn(t, scope, time.Now().UTC())

	return store, nativeTraceVisibilityMutation(t, turn)
}

func privacyState(t *testing.T, store *CanonicalStore, scope m.Scope, sourceID string) string {
	t.Helper()

	key, err := canonicalKey(scope, "source-privacy", sourceID)
	if err != nil {
		t.Fatal(err)
	}

	var record struct {
		State    string       `json:"state"`
		Scope    m.Scope      `json:"scope"`
		SourceID string       `json:"source_id"`
		Payload  m.PayloadRef `json:"payload"`
	}

	err = store.db.View(func(txn *badger.Txn) error { return getCanonicalRecord(txn, key, "source-privacy", &record) })
	if err != nil {
		t.Fatalf("source privacy missing: %v", err)
	}

	if record.Scope != scope || record.SourceID != sourceID || record.Payload.ID == "" {
		t.Fatalf("unbound authority: %+v", record)
	}

	return record.State
}

func TestSourcePrivacyAtomicLateSensitiveAndMonotone(t *testing.T) {
	store, mutation := privacyFixture(t)

	sourceID := mutation.Sources[0].SourceID
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, sourceID); got != "standard" {
		t.Fatalf("initial=%s", got)
	}

	later := mutation
	later.ExpectedScopeVersion = 1
	later.Operation.OperationID = "sensitive-later"
	later.Operation.Fingerprint = "sensitive-later"
	later.Sources = nil
	later.Messages = nil
	later.Payloads = nil

	later.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)
	later.Revisions[0].RevisionID = "sensitive-later"
	later.Revisions[0].Sensitivity = m.SensitivitySensitive
	later.Heads = []m.ItemHead{{ItemID: later.Revisions[0].ItemID, RevisionID: "sensitive-later"}}

	store.beforeCanonicalMutationCommit = func() error { return errors.New("abort") }
	if _, err := store.ApplyCanonicalMutation(context.Background(), later); err == nil {
		t.Fatal("fault ignored")
	}

	if got := privacyState(t, store, mutation.Scope, sourceID); got != "standard" {
		t.Fatalf("aborted classification published: %s", got)
	}

	store.beforeCanonicalMutationCommit = nil
	if _, err := store.ApplyCanonicalMutation(context.Background(), later); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, sourceID); got != "sensitive" {
		t.Fatalf("late sensitivity=%s", got)
	}

	later.ExpectedScopeVersion = 2
	later.Operation.OperationID = "standard-later"
	later.Operation.Fingerprint = "standard-later"
	later.Revisions[0].RevisionID = "standard-later"
	later.Revisions[0].Sensitivity = m.SensitivityStandard

	later.Heads[0].RevisionID = "standard-later"
	if _, err := store.ApplyCanonicalMutation(context.Background(), later); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, sourceID); got != "sensitive" {
		t.Fatalf("sensitivity reopened: %s", got)
	}
}

func TestSourcePrivacyUnclassifiedInitialCannotPromoteFromSubset(t *testing.T) {
	store, mutation := privacyFixture(t)
	initial := mutation
	initial.Revisions = nil
	initial.Heads = nil

	initial.Items = nil
	if _, err := store.ApplyCanonicalMutation(context.Background(), initial); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("source default certified: %s", got)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "subset"
	mutation.Operation.Fingerprint = "subset"
	mutation.Sources = nil
	mutation.Messages = nil

	mutation.Payloads = nil
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, initial.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("historic subset promoted: %s", got)
	}
}

func TestSourcePrivacyIncompleteEvidenceCannotBeHiddenByStandardSibling(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		t.Run(map[bool]string{false: "incomplete-first", true: "incomplete-last"}[reverse], func(t *testing.T) {
			store, mutation := privacyFixture(t)
			incomplete := mutation.Revisions[0]
			incomplete.RevisionID = "incomplete"
			incomplete.Evidence = append([]m.EvidenceRef(nil), incomplete.Evidence...)

			incomplete.Evidence[0].TextDigest = ""
			if reverse {
				mutation.Revisions = append(mutation.Revisions, incomplete)
			} else {
				mutation.Revisions = append([]m.MemoryRevision{incomplete}, mutation.Revisions...)
			}

			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
				t.Fatalf("incomplete sibling blessed: %s", got)
			}
		})
	}
}

func TestSourcePrivacyWritersRejectUnpreparedStore(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	key, err := m.TupleKey(canonicalNamespace, canonicalVersion, "source-privacy-ready")
	if err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Delete(key) }); err != nil {
		t.Fatal(err)
	}
	// Replay is also blocked: it may otherwise publish certificates into an old store.
	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable {
		t.Fatalf("unprepared mutation accepted: %v", err)
	}

	_, err = store.CommitCanonicalOperation(context.Background(), m.CanonicalOperationCommitRequest{
		Scope: mutation.Scope, OperationID: "empty-boundary", Fingerprint: "empty-boundary",
		ExpectedScopeVersion: 1, CommittedAt: mutation.Operation.CommittedAt})
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable {
		t.Fatalf("unprepared empty operation accepted: %v", err)
	}
}

func TestSourcePrivacySensitiveSourceAndSiblingAlwaysWin(t *testing.T) {
	for _, sourceOnly := range []bool{true, false} {
		t.Run(map[bool]string{true: "source-only", false: "incomplete-sibling"}[sourceOnly], func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if sourceOnly {
				mutation.Sources[0].Sensitivity = m.SensitivitySensitive
				mutation.Revisions = nil
				mutation.Heads = nil
			} else {
				mutation.Revisions[0].Sensitivity = m.SensitivitySensitive
				incomplete := mutation.Revisions[0]
				incomplete.RevisionID = "incomplete"
				incomplete.Sensitivity = m.SensitivityStandard
				incomplete.Evidence = append([]m.EvidenceRef(nil), incomplete.Evidence...)
				incomplete.Evidence[0].TextDigest = ""
				mutation.Revisions = append(mutation.Revisions, incomplete)
			}

			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "sensitive" {
				t.Fatalf("known sensitivity lost: %s", got)
			}
		})
	}
}

func TestSourcePrivacySensitiveProvenanceTaintsParentSources(t *testing.T) {
	store, parent := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), parent); err != nil {
		t.Fatal(err)
	}

	child := canonicalRevisionMutation(parent.Scope, 1, "child", canonicalRevision("child", "child-item"))
	// Child's direct evidence is a different, incomplete legacy source. Its
	// structural parent still carries validated authority for the original source.
	child.Revisions[0].Parents = []string{parent.Revisions[0].RevisionID}

	child.Revisions[0].Sensitivity = m.SensitivitySensitive
	if _, err := store.ApplyCanonicalMutation(context.Background(), child); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, parent.Scope, parent.Sources[0].SourceID); got != "sensitive" {
		t.Fatalf("parent source omitted: %s", got)
	}
}

func TestSourcePrivacyRejectsWrongMessageOwnershipAndScopeMetadata(t *testing.T) {
	for _, corruption := range []string{"scope", "payload", "coverage", "state"} {
		t.Run(corruption, func(t *testing.T) {
			store, mutation := privacyFixture(t)
			if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
				t.Fatal(err)
			}

			source := mutation.Sources[0]
			key, _ := canonicalKey(mutation.Scope, "source-privacy", source.SourceID)
			record := map[string]any{"version": 1, "coverage_version": 1, "scope": mutation.Scope,
				"source_id": source.SourceID, "payload": source.Payload, "state": "standard"}

			switch corruption {
			case "scope":
				record["scope"] = m.Scope{Key: mutation.Scope.Key, Kind: m.ScopeKindGroup}
			case "payload":
				record["payload"] = m.PayloadRef{ID: "forged", Digest: "forged", ByteSize: 1}
			case "coverage":
				record["coverage_version"] = 2
			case "state":
				record["state"] = "safe"
			}

			if err := store.db.Update(func(txn *badger.Txn) error {
				return putCanonicalRecord(txn, key, "source-privacy", record)
			}); err != nil {
				t.Fatal(err)
			}

			mutation.ExpectedScopeVersion = 1
			mutation.Operation.OperationID = "new-op"
			mutation.Operation.Fingerprint = "new-op"
			mutation.Revisions[0].RevisionID = "new-revision"
			mutation.Heads[0].RevisionID = "new-revision"

			_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
			if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeStoreFailure {
				t.Fatalf("invalid authority accepted: %v", err)
			}
		})
	}
}

func TestSourcePrivacyInitialAbortPublishesNoAuthority(t *testing.T) {
	store, mutation := privacyFixture(t)

	store.beforeCanonicalMutationCommit = func() error { return errors.New("initial abort") }
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err == nil {
		t.Fatal("fault ignored")
	}

	key, _ := canonicalKey(mutation.Scope, "source-privacy", mutation.Sources[0].SourceID)

	if err := store.db.View(func(txn *badger.Txn) error {
		_, err := txn.Get(key)

		return err
	}); !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("aborted metadata published: %v", err)
	}
}

func TestSourcePrivacyExactScopeAndBoundedTouchedSources(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	other := mutation
	other.Scope.Kind = m.ScopeKindGroup

	other.Sources = append([]m.CanonicalSourceRecord(nil), mutation.Sources...)
	other.Sources[0].Scope = other.Scope

	other.Items = append([]m.MemoryItem(nil), mutation.Items...)
	other.Items[0].Scope = other.Scope

	other.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)

	other.Revisions[0].Sensitivity = m.SensitivitySensitive
	if _, err := store.ApplyCanonicalMutation(context.Background(), other); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "standard" {
		t.Fatalf("scope kind contaminated: %s", got)
	}

	if got := privacyState(t, store, other.Scope, other.Sources[0].SourceID); got != "sensitive" {
		t.Fatalf("sensitivity missing: %s", got)
	}
	// An unrelated corrupt historical source must not be visited by this writer.
	key, _ := canonicalKey(mutation.Scope, "source", "unrelated-corrupt")

	if err := store.db.Update(func(txn *badger.Txn) error {
		return txn.Set(key, []byte("opaque unrelated record"))
	}); err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "bounded"
	mutation.Operation.Fingerprint = "bounded"
	mutation.Revisions[0].RevisionID = "bounded"

	mutation.Heads[0].RevisionID = "bounded"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatalf("writer scanned unrelated history: %v", err)
	}
}

func TestSourcePrivacyReopenDoesNotBlessOldStore(t *testing.T) {
	directory := t.TempDir()

	store, err := OpenCanonicalStore(directory)
	if err != nil {
		t.Fatal(err)
	}

	mutation := canonicalRevisionMutation(m.Scope{Key: "test:old", Kind: m.ScopeKindPersonal},
		0, "old", canonicalRevision("old", "old-item"))
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if err := store.db.Update(func(txn *badger.Txn) error { return txn.Delete(sourcePrivacyReadinessKey()) }); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = OpenCanonicalStore(directory)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = store.Close() })

	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeUnavailable {
		t.Fatalf("reopen blessed old history: %v", err)
	}
}

func TestSourcePrivacyRejectsUnboundedParentEvidence(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}
	// All parents exist and are individually valid. Reject their unbounded
	// combined authority before loading history, rather than scan it at ingest.
	oversized := mutation.Revisions[0]
	oversized.RevisionID = "oversized"
	oversized.Parents = make([]string, m.MaxSourcesPerRevision+1)

	parents := make([]m.MemoryRevision, len(oversized.Parents))
	for i := range parents {
		parents[i] = mutation.Revisions[0]
		parents[i].RevisionID = "parent-" + strconv.Itoa(i)
		oversized.Parents[i] = parents[i].RevisionID
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "oversized"
	mutation.Operation.Fingerprint = "oversized"

	mutation.Revisions = append(parents, oversized)
	mutation.Heads = nil

	_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeLimitExceeded {
		t.Fatalf("unbounded parents accepted: %v", err)
	}
}

func TestSourcePrivacyUnclassifiedMessageAppendInvalidatesAuthority(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "append-message"
	mutation.Operation.Fingerprint = "append-message"
	mutation.Sources = nil
	mutation.Items = nil
	mutation.Revisions = nil
	mutation.Heads = nil
	mutation.Messages = append([]m.MessageRecord(nil), mutation.Messages[:1]...)
	mutation.Messages[0].MessageID = "unclassified-message"
	mutation.Messages[0].Payload = payloadRef("unclassified-message", []byte("unclassified"))

	mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Messages[0].Payload, Data: []byte("unclassified")}}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Messages[0].SourceID); got != "unknown" {
		t.Fatalf("new unclassified text blessed: %s", got)
	}
}

func TestSourcePrivacyNewSourceWithPriorDanglingHistoryStaysUnknown(t *testing.T) {
	store, mutation := privacyFixture(t)
	dangling := mutation
	dangling.Sources = nil
	dangling.Messages = nil

	dangling.Revisions = append([]m.MemoryRevision(nil), mutation.Revisions...)

	dangling.Revisions[0].Sensitivity = m.SensitivitySensitive
	if _, err := store.ApplyCanonicalMutation(context.Background(), dangling); err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "new-source"
	mutation.Operation.Fingerprint = "new-source"
	mutation.Revisions[0].RevisionID = "new-source"

	mutation.Heads[0].RevisionID = "new-source"
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("historic classification erased by source creation: %s", got)
	}
}

func TestSourcePrivacyOpaqueOrMismatchedEmbeddedRawSourceCannotAuthorize(t *testing.T) {
	for _, opaque := range []bool{true, false} {
		t.Run(map[bool]string{true: "opaque", false: "embedded-turn"}[opaque], func(t *testing.T) {
			store, mutation := privacyFixture(t)
			payload := []byte("opaque source")

			if !opaque {
				turn := canonicalReaderTestTurn(t, mutation.Scope, time.Now().UTC())
				turn.Messages[0].Text = "DIFFERENT raw source secret"
				record := privacyEmbeddedRecord(turn)

				var err error

				payload, err = json.Marshal(record)
				if err != nil {
					t.Fatal(err)
				}
			}

			mutation.Sources[0].Payload = payloadRef("opaque-source", payload)
			mutation.Payloads[0] = m.CanonicalPayload{Ref: mutation.Sources[0].Payload, Data: payload}

			_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
			if err != nil {
				return
			} // Structural mismatch may be rejected atomically.

			if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
				t.Fatalf("unproven raw source authorized: %s", got)
			}
		})
	}
}

func TestSourcePrivacyStandardSiblingCannotCertifyMessageExtension(t *testing.T) {
	store, mutation := privacyFixture(t)
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = 1
	mutation.Operation.OperationID = "extend-with-standard"
	mutation.Operation.Fingerprint = "extend-with-standard"
	mutation.Revisions[0].RevisionID = "extend-with-standard"
	mutation.Heads[0].RevisionID = "extend-with-standard"
	mutation.Messages = append([]m.MessageRecord(nil), mutation.Messages[:1]...)
	mutation.Messages[0].MessageID = "new-unclassified-message"
	mutation.Messages[0].Payload = payloadRef("new-unclassified-message", []byte("unclassified"))

	mutation.Payloads = []m.CanonicalPayload{{Ref: mutation.Messages[0].Payload, Data: []byte("unclassified")}}
	if _, err := store.ApplyCanonicalMutation(context.Background(), mutation); err != nil {
		t.Fatal(err)
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("standard subset certified extension: %s", got)
	}
}

func TestSourcePrivacyEvidenceMustBelongToEmbeddedRawMembership(t *testing.T) {
	store, mutation := privacyFixture(t)
	// Keep all embedded raw messages consistent with the stored table, but
	// derive from an extra message that is absent from the embedded source.
	turn := canonicalReaderTestTurn(t, mutation.Scope, mutation.Operation.CommittedAt)
	raw := privacyEmbeddedRecord(turn)

	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	mutation.Sources[0].Payload = payloadRef("embedded-source", encoded)
	mutation.Payloads[0] = m.CanonicalPayload{Ref: mutation.Sources[0].Payload, Data: encoded}
	message := mutation.Messages[0]
	message.MessageID = "extra-message"
	mutation.Messages = append(mutation.Messages, message)
	mutation.Revisions[0].Evidence[0].MessageID = message.MessageID

	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		return
	}

	if got := privacyState(t, store, mutation.Scope, mutation.Sources[0].SourceID); got != "unknown" {
		t.Fatalf("evidence outside embedded membership certified: %s", got)
	}
}

func privacyEmbeddedRecord(turn m.Turn) m.SourceRecord {
	return m.SourceRecord{SchemaVersion: m.DerivedSchemaVersionV1, Ref: m.SourceRef{
		Scope: turn.Scope, ExportID: turn.ExportID, ConversationID: turn.Conversation.ConversationID,
		SourceTurnID: turn.SourceTurnID}, State: m.SourceStateActive, Turn: &turn}
}

func TestSourcePrivacyRejectsWrongMessageOwnership(t *testing.T) {
	store, mutation := privacyFixture(t)
	mutation.Messages[0].SourceID = "other-source"

	_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
		t.Fatalf("cross-source evidence accepted: %v", err)
	}
}

func TestSourcePrivacyRejectsDanglingMessageBeforeSourceCreation(t *testing.T) {
	store, mutation := privacyFixture(t)
	mutation.Sources = nil
	mutation.Items = nil
	mutation.Revisions = nil
	mutation.Heads = nil

	_, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
		t.Fatalf("dangling raw membership accepted: %v", err)
	}

	state, stateErr := store.LoadScopeState(context.Background(), mutation.Scope)
	if stateErr != nil || state.Version != 0 {
		t.Fatalf("dangling message changed scope: %+v %v", state, stateErr)
	}
}
