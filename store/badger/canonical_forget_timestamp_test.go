package badger_test

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestCanonicalForgetOutcomePreservesTimestampAcrossReplayAndRestart(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dir := t.TempDir()
	store := openCanonicalTestStore(t, dir)
	scope := memory.Scope{Key: "test:forget-time", Kind: memory.ScopeKindPersonal}
	forgottenAt := time.Date(2026, time.October, 10, 4, 5, 6, 0, time.UTC)
	request := memory.CanonicalForgetCommitRequest{
		Scope: scope, OperationID: "forget-time", Kind: memory.ForgetKindScope,
		Fingerprint: "forget-time-fingerprint", ForgottenAt: forgottenAt,
	}

	first, err := store.CommitCanonicalForget(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	if !first.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("first forgotten_at = %v, want %v", first.ForgottenAt, forgottenAt)
	}

	replay, err := store.CommitCanonicalForget(ctx, request)
	if err != nil || !replay.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("immediate replay forgotten_at = %v, err = %v", replay.ForgottenAt, err)
	}

	closeCanonicalTestStore(t, store)
	store = openCanonicalTestStore(t, dir)
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })

	restarted, err := store.CommitCanonicalForget(ctx, request)
	if err != nil || !restarted.ForgottenAt.Equal(forgottenAt) {
		t.Fatalf("restart replay forgotten_at = %v, err = %v", restarted.ForgottenAt, err)
	}
}
