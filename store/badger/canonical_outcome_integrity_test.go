package badger //nolint:testpackage // Injects checksummed corruption through private storage seams.

import (
	"context"
	"reflect"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/dgraph-io/badger/v4"
)

func TestCanonicalReplayRejectsOutcomeOutsideDurableScopeHistory(t *testing.T) {
	for _, kind := range []string{"operation", "mutation"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			store, err := OpenCanonicalStore(t.TempDir())
			requireEraseTestNoError(t, err)
			t.Cleanup(func() { _ = store.Close() })

			scope := memory.Scope{Key: "test:outcome-binding-" + kind, Kind: memory.ScopeKindPersonal}
			operationID := "bound-" + kind
			fingerprint := "fingerprint-" + kind

			var replay func() error

			if kind == "operation" {
				request := memory.CanonicalOperationCommitRequest{
					Scope: scope, OperationID: operationID, Fingerprint: fingerprint,
					CommittedAt: time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC),
				}
				_, err = store.CommitCanonicalOperation(ctx, request)
				replay = func() error {
					_, replayErr := store.CommitCanonicalOperation(ctx, request)

					return replayErr
				}
			} else {
				mutation := canonicalRevisionMutation(scope, 0, operationID,
					canonicalRevision("bound-revision", "bound-item"))
				mutation.Operation.Fingerprint = fingerprint
				_, err = store.ApplyCanonicalMutation(ctx, mutation)
				replay = func() error {
					_, replayErr := store.ApplyCanonicalMutation(ctx, mutation)

					return replayErr
				}
			}

			requireEraseTestNoError(t, err)

			key, err := badgerOperationKey(scope, operationID)
			requireEraseTestNoError(t, err)
			requireEraseTestNoError(t, store.db.Update(func(txn *badger.Txn) error {
				var stored badgerCanonicalOperation
				if getErr := getCanonicalRecord(txn, key, badgerRecordOperation, &stored); getErr != nil {
					return getErr
				}

				stored.Outcome.ScopeVersion = 999

				return putCanonicalRecord(txn, key, badgerRecordOperation, stored)
			}))

			assertCanonicalStoreFailure(t, replay())
			assertCanonicalStoreFailure(t, store.VerifyCanonicalIntegrity(ctx))
		})
	}
}

func TestCanonicalReplayClassifiesMalformedStoredOutcomeAsStoreFailure(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "test:malformed-outcome", Kind: memory.ScopeKindPersonal}
	request := memory.CanonicalOperationCommitRequest{
		Scope: scope, OperationID: "malformed", Fingerprint: "malformed-fingerprint",
		CommittedAt: time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC),
	}
	_, err = store.CommitCanonicalOperation(ctx, request)
	requireEraseTestNoError(t, err)

	key, err := badgerOperationKey(scope, request.OperationID)
	requireEraseTestNoError(t, err)
	requireEraseTestNoError(t, store.db.Update(func(txn *badger.Txn) error {
		return putCanonicalRecord(txn, key, badgerRecordOperation, badgerCanonicalOperation{
			Fingerprint: request.Fingerprint,
			Outcome:     memory.CanonicalMutationOutcome{ChangeSeq: 1},
		})
	}))

	_, err = store.CommitCanonicalOperation(ctx, request)
	assertCanonicalStoreFailure(t, err)
}

func TestCanonicalReplayAcceptsLegacyChangeWithoutScopeVersion(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "test:legacy-change-binding", Kind: memory.ScopeKindPersonal}
	request := memory.CanonicalOperationCommitRequest{
		Scope: scope, OperationID: "legacy-change", Fingerprint: "legacy-change-fingerprint",
		CommittedAt: time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC),
	}
	want, err := store.CommitCanonicalOperation(ctx, request)
	requireEraseTestNoError(t, err)

	changeKey, err := badgerScopeChangeKey(scope, want.ChangeSeq)
	requireEraseTestNoError(t, err)
	requireEraseTestNoError(t, store.db.Update(func(txn *badger.Txn) error {
		var change memory.ScopeChange
		if getErr := getCanonicalRecord(txn, changeKey, badgerRecordChange, &change); getErr != nil {
			return getErr
		}

		change.ScopeVersion = 0

		return putCanonicalRecord(txn, changeKey, badgerRecordChange, change)
	}))

	got, err := store.CommitCanonicalOperation(ctx, request)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("legacy replay = %+v, err = %v; want %+v", got, err, want)
	}
}

func TestLoadCanonicalOperationRejectsMissingBoundChange(t *testing.T) {
	ctx := context.Background()
	store, err := OpenCanonicalStore(t.TempDir())
	requireEraseTestNoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	scope := memory.Scope{Key: "test:missing-bound-change", Kind: memory.ScopeKindPersonal}
	request := memory.CanonicalOperationCommitRequest{
		Scope: scope, OperationID: "missing-change", Fingerprint: "missing-change-fingerprint",
		CommittedAt: time.Date(2026, time.October, 14, 1, 2, 3, 0, time.UTC),
	}
	outcome, err := store.CommitCanonicalOperation(ctx, request)
	requireEraseTestNoError(t, err)
	changeKey, err := badgerScopeChangeKey(scope, outcome.ChangeSeq)
	requireEraseTestNoError(t, err)
	requireEraseTestNoError(t, store.db.Update(func(txn *badger.Txn) error { return txn.Delete(changeKey) }))

	_, found, err := store.LoadCanonicalOperation(ctx, scope, request.OperationID)
	if found {
		t.Fatal("operation with a missing bound change reported as found")
	}

	assertCanonicalStoreFailure(t, err)
}

func assertCanonicalStoreFailure(t *testing.T, err error) {
	t.Helper()

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent {
		t.Fatalf("error = %v, classification = %s/%s/%t; want store_failure/permanent/true", err, code, class, ok)
	}
}
