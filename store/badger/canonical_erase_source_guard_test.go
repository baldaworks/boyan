package badger //nolint:testpackage // Exercises source-specific use of the durable erase ownership protocol.

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestSourceEraseSelectsOnlyItsSourceAndDerivedRecords(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)

	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:source-select", Kind: memory.ScopeKindPersonal}
	refs := seedEraseHistory(t, store, scope, 2)
	request := memory.ScopeEraseRequest{
		Scope: scope, OperationID: "source-erase", Fingerprint: "fp", ForgottenAt: time.Now(), Kind: memory.ForgetKindSource,
		SourceID: memory.TurnSourceID("export-0000"), SourceRef: memory.SourceRef{
			Scope:          scope,
			ExportID:       "export-0000",
			ConversationID: "conversation",
			SourceTurnID:   "export-0000",
		},
	}

	journal, err := store.RunScopeErase(context.Background(), request)
	requireEraseTestNoError(t, err)

	if journal.Outcome.Kind != memory.ForgetKindSource || len(journal.Outcome.Sources) != 1 ||
		journal.SourceCount != 1 || journal.RevisionCount != 1 {
		t.Fatalf("journal=%+v", journal)
	}

	for _, ref := range refs[:3] {
		if _, err := store.LoadPayload(context.Background(), ref); err == nil {
			t.Fatalf("source payload survived: %s", ref.ID)
		}
	}

	for _, ref := range refs[3:] {
		if _, err := store.LoadPayload(context.Background(), ref); err != nil {
			t.Fatalf("other source payload lost: %s: %v", ref.ID, err)
		}
	}
}

func TestSourceEraseRejectsSharedLegacyBlobBeforeDenial(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:source-shared", Kind: memory.ScopeKindPersonal}
	other := scope
	other.Kind = memory.ScopeKindGroup
	refs := seedEraseHistory(t, store, scope, 1)
	sourceID := memory.TurnSourceID("export-0000")
	err = store.db.Update(func(txn *badger.Txn) error {
		source := memory.CanonicalSourceRecord{SourceID: sourceID, Scope: other,
			Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: refs[0]}

		return putBadgerCanonicalRecord(txn, other, badgerRecordSource, sourceID, source)
	})
	requireEraseTestNoError(t, err)

	request := memory.ScopeEraseRequest{
		Scope: scope, OperationID: "source-erase", Fingerprint: "fp", ForgottenAt: time.Now(), Kind: memory.ForgetKindSource,
		SourceID: sourceID, SourceRef: memory.SourceRef{
			Scope:          scope,
			ExportID:       "export-0000",
			ConversationID: "conversation",
			SourceTurnID:   "export-0000",
		},
	}

	journal, err := store.RunScopeErase(context.Background(), request)
	if err == nil || memory.IsRetryable(err) || journal.Stage != memory.ScopeEraseRejected {
		t.Fatalf("journal=%+v err=%v", journal, err)
	}

	denied, err := store.IsSourceDenied(context.Background(), scope, sourceID)
	if err != nil || denied {
		t.Fatalf("shared source denied=%t err=%v", denied, err)
	}

	_, err = store.LoadPayload(context.Background(), refs[0])
	requireEraseTestNoError(t, err)

	pending, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}
}

func TestRejectedEraseFailedFinalCommitDoesNotExposeTerminalState(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:reject-commit", Kind: memory.ScopeKindPersonal}
	seedSharedEraseSource(t, store, scope)
	request := memory.ScopeEraseRequest{Scope: scope, OperationID: "reject", Fingerprint: "fp", ForgottenAt: time.Now()}
	store.beforeScopeEraseBatchCommit = func(journal memory.ScopeEraseJournal) error {
		if journal.Stage == memory.ScopeEraseRejected {
			return context.Canceled
		}

		return nil
	}

	journal, err := store.RunScopeErase(context.Background(), request)
	if err == nil || journal.Stage != "" {
		t.Fatalf("failed commit exposed stage=%s err=%v", journal.Stage, err)
	}

	journal, found, err := store.LoadScopeErase(context.Background(), scope, "reject")
	requireEraseTestNoError(t, err)

	if !found || journal.Stage != memory.ScopeEraseRejectLocks {
		t.Fatalf("durable rejection progress=%+v found=%t", journal, found)
	}

	store.beforeScopeEraseBatchCommit = nil

	journal, err = store.RunScopeErase(context.Background(), request)
	if err == nil || journal.Stage != memory.ScopeEraseRejected {
		t.Fatalf("retry=%+v err=%v", journal, err)
	}
}

func seedSharedEraseSource(t *testing.T, store *CanonicalStore, scope memory.Scope) {
	t.Helper()
	refs := seedEraseHistory(t, store, scope, 1)
	other := scope
	other.Kind = memory.ScopeKindGroup
	err := store.db.Update(func(txn *badger.Txn) error {
		source := memory.CanonicalSourceRecord{
			SourceID: memory.TurnSourceID("export-0000"), Scope: other, Sensitivity: memory.SensitivityStandard,
			Retention: memory.RetentionClassStandard, Payload: refs[0],
		}

		return putBadgerCanonicalRecord(txn, other, badgerRecordSource, source.SourceID, source)
	})
	requireEraseTestNoError(t, err)
}

func TestSourceEraseReferenceLimitReleasesFenceBeforeDenial(t *testing.T) {
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "telegram:source-limit", Kind: memory.ScopeKindPersonal}
	seedEraseHistory(t, store, scope, 1)
	seedSourceRevisionFanout(t, store, scope, memory.MaxScopeEraseReferences)
	request := memory.ScopeEraseRequest{
		Scope: scope, OperationID: "source-limit", Fingerprint: "fp", ForgottenAt: time.Now(), Kind: memory.ForgetKindSource,
		SourceID: memory.TurnSourceID("export-0000"),
		SourceRef: memory.SourceRef{Scope: scope, ExportID: "export-0000",
			ConversationID: "conversation", SourceTurnID: "export-0000"},
	}
	journal, err := store.RunScopeErase(context.Background(), request)

	var typed *memory.Error
	if !errors.As(err, &typed) || typed.Code != memory.CodeLimitExceeded || journal.Stage != memory.ScopeEraseRejected {
		t.Fatalf("limit rejection=%+v err=%v", journal, err)
	}

	denied, err := store.IsSourceDenied(context.Background(), scope, request.SourceID)
	if err != nil || denied {
		t.Fatalf("source denied before limit rejection=%t err=%v", denied, err)
	}

	pending, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending after limit rejection=%d err=%v", len(pending), err)
	}
}

func seedSourceRevisionFanout(t *testing.T, store *CanonicalStore, scope memory.Scope, count int) {
	t.Helper()

	err := store.db.Update(func(txn *badger.Txn) error {
		var revision memory.MemoryRevision

		key, err := canonicalKey(scope, badgerRecordRevision, "revision-0000")
		if err != nil {
			return err
		}

		if err := getCanonicalRecord(txn, key, badgerRecordRevision, &revision); err != nil {
			return err
		}

		for index := 1; index < count; index++ {
			revision.RevisionID = fmt.Sprintf("fanout-%04d", index)
			if err := putBadgerCanonicalRecord(txn, scope, badgerRecordRevision, revision.RevisionID, revision); err != nil {
				return err
			}
		}

		return nil
	})
	requireEraseTestNoError(t, err)
}
