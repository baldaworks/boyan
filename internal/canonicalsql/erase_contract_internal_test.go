//go:build integration

package canonicalsql

import (
	"context"
	"errors"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"testing"
	"time"
)

func TestEraseLegacyOutcomeSelectsOnlyCommittedTargets(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		scope := memory.Scope{Key: "erase:legacy", Kind: memory.ScopeKindPersonal}
		refs := seedEraseHistory(t, o, scope, 2)
		at := time.Now()
		sourceID := memory.TurnSourceID("export-0000")
		requireEraseTestNoError(t, o.DenySource(t.Context(), scope, sourceID, at))
		requireEraseTestNoError(t, o.DenyRevision(t.Context(), scope, "revision-0000", at))
		request := memory.CanonicalForgetCommitRequest{Scope: scope, OperationID: "legacy", Fingerprint: "fp",
			Kind: memory.ForgetKindScope, ForgottenAt: at,
			Sources: []memory.SourceRef{{Scope: scope, ExportID: "export-0000", ConversationID: "conversation",
				SourceTurnID: "export-0000"}},
			Revisions: []memory.RevisionRef{{ItemID: "item", RevisionID: "revision-0000"}}}
		original, err := o.CommitCanonicalForget(t.Context(), request)
		requireEraseTestNoError(t, err)
		requireEraseTestNoError(t, o.ScrubCanonicalForget(t.Context(), scope, []string{sourceID}, []string{"revision-0000"}))
		requireEraseTestNoError(t, o.Close())
		o = reopen()
		j, err := o.RunScopeErase(t.Context(), memory.ScopeEraseRequest{Scope: scope, OperationID: "legacy",
			Fingerprint: "fp", ExpectedScopeVersion: 1, ForgottenAt: at})
		requireEraseTestNoError(t, err)

		if !j.LegacyOutcome || !reflect.DeepEqual(*j.Outcome, original) {
			t.Fatalf("legacy outcome changed %+v", j)
		}

		requireErasePayloadsAbsent(t, o, refs[:3])

		for _, ref := range refs[3:] {
			_, err := o.LoadPayload(t.Context(), ref)
			requireEraseTestNoError(t, err)
		}

		state, err := o.LoadScopeState(t.Context(), scope)
		if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
			t.Fatalf("legacy advanced state %+v %v", state, err)
		}
	})
}

func TestEraseFenceAllWritesCancellationAndCorruption(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, _ func() *Owner) {
		h := prepareTraceHistory(t, o)
		r := erasePrivacyRequest(h)
		canceled, cancel := context.WithCancel(t.Context())
		cancel()

		_, err := o.BeginScopeErase(canceled, r)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel=%v", err)
		}

		_, found, err := o.LoadScopeErase(t.Context(), h.scope, "erase")
		if err != nil || found {
			t.Fatalf("canceled begin journal=%t %v", found, err)
		}

		_, err = o.BeginScopeErase(t.Context(), r)
		requireEraseTestNoError(t, err)

		checks := []func() error{
			func() error { return o.DenySource(t.Context(), h.scope, h.source.SourceID, time.Now()) },
			func() error { return o.DenyRevision(t.Context(), h.scope, h.revision.RevisionID, time.Now()) },
			func() error { return o.PutPayload(t.Context(), h.payloads[0].Data, h.payloads[0].Ref) },
			func() error { return o.DeletePayload(t.Context(), h.payloads[0].Ref) },
			func() error {
				_, err := o.ApplyCanonicalMutation(t.Context(), historyMutation(h))

				return err
			},
			func() error {
				_, err := o.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{Scope: h.scope,
					OperationID: "foreign", Fingerprint: "fp", CommittedAt: time.Now()})

				return err
			},
			func() error {
				_, err := o.CommitCanonicalForget(t.Context(), memory.CanonicalForgetCommitRequest{Scope: h.scope,
					OperationID: "foreign", Fingerprint: "fp", Kind: memory.ForgetKindScope, ForgottenAt: time.Now()})

				return err
			},
			func() error { return o.CompleteScopeErase(t.Context(), h.scope, "erase") },
		}
		for _, check := range checks {
			if err := check(); !memory.IsRetryable(err) {
				t.Fatalf("writer passed pending fence: %v", err)
			}
		}

		other := h.scope
		other.Kind = memory.ScopeKindGroup
		requireEraseTestNoError(t, o.DenySource(t.Context(), other, "unrelated", time.Now()))
		_, err = o.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{Scope: other,
			OperationID: "other", Fingerprint: "fp", CommittedAt: time.Now()})
		requireEraseTestNoError(t, err)

		assertCorruptEraseRemainsFenced(t, o, h.scope)
	})
}

func TestErasePendingRawPagesAndInvalidCursors(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		scopes := []memory.Scope{{Key: "erase:a", Kind: memory.ScopeKindGroup}, {Key: "erase:a",
			Kind: memory.ScopeKindPersonal}, {Key: "erase:aa", Kind: memory.ScopeKindPersonal}}
		for _, scope := range scopes {
			_, err := o.BeginScopeErase(t.Context(), memory.ScopeEraseRequest{Scope: scope, OperationID: "erase",
				Fingerprint: "fp", ForgottenAt: time.Now()})
			requireEraseTestNoError(t, err)
		}

		var cursor []byte
		for _, scope := range scopes {
			pages, next, err := o.ListPendingScopeErases(t.Context(), cursor, 1)
			if err != nil || len(pages) != 1 || pages[0].Scope != scope {
				t.Fatalf("page=%+v %v", pages, err)
			}

			cursor = next

			requireEraseTestNoError(t, o.Close())
			o = reopen()
		}

		if len(cursor) != 0 {
			t.Fatal("final page retains cursor")
		}

		if _, _, err := o.ListPendingScopeErases(t.Context(), []byte("bad"), 1); err == nil {
			t.Fatal("invalid cursor accepted")
		}

		if _, _, err := o.ListPendingScopeErases(t.Context(), nil, 0); err == nil {
			t.Fatal("invalid page accepted")
		}
	})
}

func assertCorruptEraseRemainsFenced(t *testing.T, o *Owner, scope memory.Scope) {
	t.Helper()

	key, _ := privacyKey(scope, EraseJournal, "erase")

	requireEraseTestNoError(t, o.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		return putPrivacyRecord(ctx, tx, key, EraseJournal, map[string]any{"version": 99})
	}))
	_, _, err := o.LoadScopeErase(t.Context(), scope, "erase")
	assertCode(t, err, memory.CodeStoreFailure)
	err = o.DenySource(t.Context(), scope, "source", time.Now())
	assertCode(t, err, memory.CodeStoreFailure)

	if err := o.CompleteScopeErase(t.Context(), scope, "erase"); err == nil {
		t.Fatal("corrupt journal completed")
	}
}
