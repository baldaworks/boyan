//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeEraseRejectsLegacySharedBlobBeforeDenialAndReleasesFence(t *testing.T) {
	forEachOwner(t, func(t *testing.T, store *Owner, _ func() *Owner) {
		var err error

		scope := memory.Scope{Key: "telegram:shared", Kind: memory.ScopeKindPersonal}
		other := scope
		other.Kind = memory.ScopeKindGroup
		refs := seedEraseHistory(t, store, scope, 1)
		sourceID := memory.TurnSourceID("export-0000")

		err = store.Update(t.Context(), func(ctx context.Context, txn *Tx) error {
			source := memory.CanonicalSourceRecord{SourceID: sourceID, Scope: other,
				Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: refs[0]}

			return putEraseFixtureRecord(ctx, txn, other, Source, sourceID, source)
		})
		requireEraseTestNoError(t, err)

		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		journal, err := store.RunScopeErase(context.Background(), request)

		var typed *memory.Error
		if !errors.As(err, &typed) || typed.Code != memory.CodeConflict || memory.IsRetryable(err) ||
			journal.Stage != memory.ScopeEraseRejected {
			t.Fatalf("journal=%+v error=%v", journal, err)
		}

		denied, err := store.IsSourceDenied(context.Background(), scope, sourceID)
		if err != nil || denied {
			t.Fatalf("denied=%t err=%v", denied, err)
		}

		_, err = store.LoadPayload(context.Background(), refs[0])
		requireEraseTestNoError(t, err)

		pending, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending=%v err=%v", pending, err)
		}

		if err := store.DenySource(context.Background(), scope, "unrelated", time.Now()); err != nil {
			t.Fatalf("fence survived rejection: %v", err)
		}
	})
}

func TestScopeEraseBlobLockRejectsForeignReferenceInsertion(t *testing.T) {
	forEachOwner(t, func(t *testing.T, store *Owner, _ func() *Owner) {
		var err error

		scope := memory.Scope{Key: "telegram:locked", Kind: memory.ScopeKindPersonal}
		refs := seedEraseHistory(t, store, scope, 1)
		requireEraseTestNoError(t, store.PrepareSourcePrivacy(t.Context()))

		request := memory.ScopeEraseRequest{Scope: scope, OperationID: "erase", Fingerprint: "fp", ForgottenAt: time.Now()}
		_, err = store.BeginScopeErase(context.Background(), request)
		requireEraseTestNoError(t, err)

		advanceEraseTestToStage(t, store, scope, "erase", memory.ScopeEraseLockMessages)

		other := scope
		other.Kind = memory.ScopeKindGroup

		mutation := memory.CanonicalMutation{
			SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: other,
			Operation: memory.OperationRecord{OperationID: "foreign", Fingerprint: "fp", CommittedAt: time.Now()},
			Sources: []memory.CanonicalSourceRecord{{SourceID: "foreign", Scope: other,
				Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: refs[0]}},
		}
		_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
		assertCode(t, err, memory.CodeConflict)

		if !memory.IsRetryable(err) {
			t.Fatalf("foreign reference insertion=%v", err)
		}
	})
}

func TestScopeEraseCompetingBlobOwnersReleaseRatherThanDeadlock(t *testing.T) {
	forEachOwner(t, func(t *testing.T, store *Owner, _ func() *Owner) {
		var err error

		scope := memory.Scope{Key: "telegram:competing", Kind: memory.ScopeKindPersonal}
		other := scope
		other.Kind = memory.ScopeKindGroup

		refs := seedEraseHistory(t, store, scope, 1)
		if err := store.Update(t.Context(), func(ctx context.Context, txn *Tx) error {
			source := memory.CanonicalSourceRecord{SourceID: "foreign-source", Scope: other,
				Sensitivity: memory.SensitivityStandard, Retention: memory.RetentionClassStandard, Payload: refs[1]}

			message := memory.MessageRecord{MessageID: "foreign-message", SourceID: source.SourceID,
				Role: memory.MessageRoleUser, Payload: refs[0]}
			if err := putEraseFixtureRecord(ctx, txn, other, Source, source.SourceID, source); err != nil {
				return err
			}

			return putEraseFixtureRecord(ctx, txn, other, Message, message.MessageID, message)
		}); err != nil {
			t.Fatal(err)
		}

		requests := []memory.ScopeEraseRequest{
			{Scope: scope, OperationID: "erase-a", Fingerprint: "fp-a", ForgottenAt: time.Now()},
			{Scope: other, OperationID: "erase-b", Fingerprint: "fp-b", ForgottenAt: time.Now()},
		}
		for _, request := range requests {
			_, err = store.BeginScopeErase(context.Background(), request)
			requireEraseTestNoError(t, err)

			advanceEraseTestToStage(t, store, request.Scope, request.OperationID, memory.ScopeEraseLockMessages)
		}

		for _, request := range requests {
			_, err = store.AdvanceScopeErase(context.Background(), request.Scope, request.OperationID)
			requireEraseTestNoError(t, err)
		}

		for _, request := range requests {
			journal, err := store.RunScopeErase(context.Background(), request)
			if err == nil ||
				journal.Stage != memory.ScopeEraseRejected {
				t.Fatalf("journal=%+v err=%v", journal, err)
			}
		}

		pending, _, err := store.ListPendingScopeErases(context.Background(), nil, 10)
		if err != nil || len(pending) != 0 {
			t.Fatalf("pending=%d err=%v", len(pending), err)
		}
	})
}
