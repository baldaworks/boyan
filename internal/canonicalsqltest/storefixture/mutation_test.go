//go:build integration

package storefixture_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type mutationStore interface {
	memory.CanonicalOperationReader
	memory.CanonicalOperationCommitter
	PrepareSourcePrivacy(ctx context.Context) error
	ApplyCanonicalMutation(ctx context.Context, mutation memory.CanonicalMutation) (memory.CanonicalMutationOutcome, error)
	LoadScopeState(ctx context.Context, scope memory.Scope) (memory.ScopeState, error)
	ScanScopeChanges(ctx context.Context, scope memory.Scope, after uint64, limit uint32) ([]memory.ScopeChange, error)
	LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error)
	LoadCanonicalRevisions(ctx context.Context,
		request memory.CanonicalRevisionReadRequest) ([]memory.MemoryRevision, error)
}

func TestPublicMutationCommitReplay(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store, ok := fixture.Store.(mutationStore)
		if !ok {
			t.Fatal("public SQL owner lacks atomic mutation contract")
		}

		if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		scope := memory.Scope{Key: "test:mutation", Kind: memory.ScopeKindPersonal}
		request := memory.CanonicalOperationCommitRequest{Scope: scope, OperationID: "empty", Fingerprint: "fp",
			CommittedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}

		outcome, err := store.CommitCanonicalOperation(t.Context(), request)
		if err != nil || !reflect.DeepEqual(outcome, memory.CanonicalMutationOutcome{ScopeVersion: 1, ChangeSeq: 1}) {
			t.Fatalf("empty commit: %+v %v", outcome, err)
		}

		replayed, err := store.CommitCanonicalOperation(t.Context(), request)
		if err != nil || !reflect.DeepEqual(replayed, outcome) {
			t.Fatalf("stale replay: %+v %v", replayed, err)
		}

		assertEmptyOperationIdentity(t, store, request, outcome)

		changes, err := store.ScanScopeChanges(t.Context(), scope, 0, 10)
		if err != nil || len(changes) != 1 || changes[0].OperationID != "empty" {
			t.Fatalf("changes: %+v %v", changes, err)
		}

		assertMutationReopen(t, fixture, scope)
	})
}

func assertEmptyOperationIdentity(t *testing.T, store mutationStore,
	request memory.CanonicalOperationCommitRequest, first memory.CanonicalMutationOutcome,
) {
	t.Helper()

	stored, found, err := store.LoadCanonicalOperation(t.Context(), request.Scope, request.OperationID)
	if err != nil || !found || stored.Fingerprint != request.Fingerprint || !reflect.DeepEqual(stored.Outcome, first) {
		t.Fatalf("stored operation fingerprint/outcome: %+v found=%v err=%v", stored, found, err)
	}

	request.Fingerprint += "-conflict"
	_, err = store.CommitCanonicalOperation(t.Context(), request)
	assertMutationCode(t, err, memory.CodeConflict)
}

func assertMutationReopen(t *testing.T, fixture *storefixture.Fixture, scope memory.Scope) {
	t.Helper()

	if err := fixture.Store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := fixture.Open(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	state, err := reopened.(mutationStore).LoadScopeState(t.Context(), scope)
	if err != nil || state.Version != 1 || state.ChangeSeq != 1 {
		t.Fatalf("reopened state: %+v %v", state, err)
	}
}
