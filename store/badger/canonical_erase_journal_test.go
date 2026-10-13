package badger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeEraseJournalSurvivesRestartAndFencesExactScope(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	store := openCanonicalTestStore(t, dir)
	scope := memory.Scope{Key: "telegram:erase", Kind: memory.ScopeKindPersonal}
	request := memory.ScopeEraseRequest{
		Scope:       scope,
		OperationID: "erase-1",
		Fingerprint: "erase-fingerprint",
		ForgottenAt: time.Now().UTC(),
	}

	journal, err := store.BeginScopeErase(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	if journal.Stage != memory.ScopeErasePrepared || journal.BaselineVersion != 0 {
		t.Fatalf("journal = %+v", journal)
	}

	closeCanonicalTestStore(t, store)
	store = openCanonicalTestStore(t, dir)
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })

	replay, err := store.BeginScopeErase(ctx, request)
	if err != nil || !replay.ForgottenAt.Equal(journal.ForgottenAt) {
		t.Fatalf("replay=%+v, err=%v", replay, err)
	}

	request.OperationID = "erase-2"
	_, err = store.BeginScopeErase(ctx, request)
	requireEraseConflict(t, err)
	err = store.DenySource(ctx, scope, "source", time.Now())
	requireEraseConflict(t, err)
	err = store.DenyRevision(ctx, scope, "revision", time.Now())
	requireEraseConflict(t, err)

	other := scope

	other.Kind = memory.ScopeKindGroup
	if err := store.DenySource(ctx, other, "source", time.Now()); err != nil {
		t.Fatal(err)
	}

	if err := store.CompleteScopeErase(ctx, scope, "erase-1"); err == nil {
		t.Fatal("unfinished erase completed")
	}

	if _, err := store.CommitCanonicalOperation(ctx, memory.CanonicalOperationCommitRequest{
		Scope:       scope,
		OperationID: "op",
		Fingerprint: "fp",
		CommittedAt: time.Now(),
	}); err == nil {
		t.Fatal("operation passed erase fence")
	}
}

func requireEraseConflict(t *testing.T, err error) {
	t.Helper()

	var typed *memory.Error
	if !errors.As(err, &typed) || typed.Code != memory.CodeConflict || !memory.IsRetryable(err) {
		t.Fatalf("expected retryable conflict, got %v", err)
	}
}

func TestScopeEraseFenceBlocksAllForeignStructuralWrites(t *testing.T) {
	ctx := context.Background()
	store := openCanonicalTestStore(t, t.TempDir())
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })

	scope := memory.Scope{Key: "telegram:erase", Kind: memory.ScopeKindPersonal}
	now := time.Now().UTC()

	_, err := store.BeginScopeErase(ctx, memory.ScopeEraseRequest{
		Scope:       scope,
		OperationID: "erase",
		Fingerprint: "fp",
		ForgottenAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}

	mutation := canonicalRevisionMutation(scope, 0, "native", canonicalRevision("revision", "item"))
	_, err = store.ApplyCanonicalMutation(ctx, mutation)
	requireEraseConflict(t, err)
	_, err = store.CommitCanonicalForget(ctx, memory.CanonicalForgetCommitRequest{
		Scope:       scope,
		OperationID: "foreign",
		Kind:        memory.ForgetKindScope,
		Fingerprint: "fp",
		ForgottenAt: now,
	})
	requireEraseConflict(t, err)
	_, err = store.CommitCanonicalOperation(ctx, memory.CanonicalOperationCommitRequest{
		Scope:       scope,
		OperationID: "boundary",
		Fingerprint: "fp",
		CommittedAt: now,
	})
	requireEraseConflict(t, err)

	other := scope

	other.Key = "telegram:other"
	if _, err := store.CommitCanonicalOperation(ctx, memory.CanonicalOperationCommitRequest{
		Scope:       other,
		OperationID: "boundary",
		Fingerprint: "fp",
		CommittedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestScopeEraseCanceledBeginCreatesNoJournal(t *testing.T) {
	ctx := context.Background()
	store := openCanonicalTestStore(t, t.TempDir())
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })

	other := memory.Scope{Key: "telegram:canceled", Kind: memory.ScopeKindPersonal}
	now := time.Now().UTC()

	canceled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := store.BeginScopeErase(canceled, memory.ScopeEraseRequest{
		Scope:       other,
		OperationID: "canceled",
		Fingerprint: "fp",
		ForgottenAt: now,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled begin = %v", err)
	}

	if _, found, err := store.LoadScopeErase(ctx, other, "canceled"); err != nil || found {
		t.Fatalf("canceled journal found=%t err=%v", found, err)
	}
}
