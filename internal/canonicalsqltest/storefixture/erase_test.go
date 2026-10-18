package storefixture_test

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type forgetStore interface {
	memory.ScopeEraseStore
	memory.CanonicalSourceForgetStore
	memory.CanonicalForgetEnumerator
	memory.CanonicalSourceIdentityResolver
	memory.CanonicalForgetOutcomeStore
	LoadPayload(ctx context.Context, ref memory.PayloadRef) ([]byte, error)
	PutPayload(ctx context.Context, data []byte, ref memory.PayloadRef) error
}

func TestPublicEraseSourceAndScope(t *testing.T) {
	for _, kind := range []memory.ForgetKind{memory.ForgetKindSource, memory.ForgetKindScope} {
		t.Run(string(kind), func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, mstore mutationStore, _ mutationProbe) {
				runPublicSourceScopeErase(t, f, mstore, kind)
			})
		})
	}
}

func TestPublicEraseJournalRecovery(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		store, ok := fixture.Store.(memory.ScopeEraseStore)
		if !ok {
			t.Fatal("public SQL owner lacks durable erase contract")
		}

		request := memory.ScopeEraseRequest{Scope: memory.Scope{Key: "erase:empty", Kind: memory.ScopeKindPersonal},
			OperationID: "erase", Fingerprint: "fingerprint", ForgottenAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}

		journal, err := store.BeginScopeErase(t.Context(), request)
		if err != nil || journal.Stage != memory.ScopeErasePrepared {
			t.Fatalf("begin: %+v %v", journal, err)
		}

		if err := store.CompleteScopeErase(t.Context(), request.Scope, request.OperationID); !memory.IsRetryable(err) {
			t.Fatalf("premature completion: %v", err)
		}

		if err := fixture.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := fixture.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()

		assertPublicEraseRecovery(t, reopened, request)
	})
}

func runPublicSourceScopeErase(t *testing.T, f *storefixture.Fixture, mstore mutationStore, kind memory.ForgetKind) {
	t.Helper()

	store, ok := f.Store.(forgetStore)
	if !ok {
		t.Fatal("public SQL owner lacks forget identity/enumeration contract")
	}

	mutation := nativeMutation(t)
	if _, err := mstore.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	refs, _, err := store.ListCanonicalSourceRefs(t.Context(), mutation.Scope, "", 10)
	if err != nil || len(refs) != 1 {
		t.Fatalf("sources=%+v err=%v", refs, err)
	}

	id, err := store.CanonicalSourceID(t.Context(), mutation.Scope, refs[0])
	requireForgetSuccess(t, err)

	if id != mutation.Sources[0].SourceID {
		t.Fatalf("id=%s err=%v", id, err)
	}

	request := memory.ScopeEraseRequest{Scope: mutation.Scope, OperationID: "forget", Fingerprint: "fp",
		ExpectedScopeVersion: 1, ForgottenAt: time.Now(), Kind: kind}
	if kind == memory.ForgetKindSource {
		request.SourceID = id
		request.SourceRef = refs[0]
	}

	journal, err := store.RunScopeErase(t.Context(), request)
	requireForgetSuccess(t, err)

	if journal.Stage != memory.ScopeEraseComplete || journal.SourceCount != 1 || journal.RevisionCount != 1 {
		t.Fatalf("erase=%+v err=%v", journal, err)
	}

	assertPublicErasePayloads(t, store, mutation)
	assertPublicEraseReplay(t, f, mstore, store, mutation)
}

func assertPublicEraseRecovery(t *testing.T, reopened storefixture.Store, request memory.ScopeEraseRequest) {
	t.Helper()

	store := reopened.(memory.ScopeEraseStore)

	pending, _, err := store.ListPendingScopeErases(t.Context(), nil, 1)
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending: %+v %v", pending, err)
	}

	journal, err := store.RunScopeErase(t.Context(), request)
	if err != nil || journal.Stage != memory.ScopeEraseComplete || journal.Outcome == nil ||
		journal.Outcome.ScopeVersion != 1 {
		t.Fatalf("run: %+v %v", journal, err)
	}

	replay, err := store.RunScopeErase(t.Context(), request)
	if err != nil || replay.Outcome.ScopeVersion != 1 || !replay.ForgottenAt.Equal(request.ForgottenAt) {
		t.Fatalf("replay: %+v %v", replay, err)
	}
}

func assertPublicErasePayloads(t *testing.T, store forgetStore,
	mutation memory.CanonicalMutation) {
	t.Helper()

	for _, p := range mutation.Payloads {
		if _, err := store.LoadPayload(t.Context(), p.Ref); err == nil {
			t.Fatal("scrubbed payload survived")
		}

		if err := store.PutPayload(t.Context(), p.Data, p.Ref); err == nil {
			t.Fatal("erased payload resurrected")
		}
	}
}
func assertPublicEraseReplay(t *testing.T, f *storefixture.Fixture, mstore mutationStore,
 store forgetStore, mutation memory.CanonicalMutation) {
	t.Helper()
	// The original contract permits an exact committed outcome replay; it must
	// not recreate payloads. A new operation cannot attach the denied identity.
	if _, err := mstore.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	for _, p := range mutation.Payloads {
		if _, err := store.LoadPayload(t.Context(), p.Ref); err == nil {
			t.Fatal("replay recreated erased payload")
		}
	}

	mutation.Operation.OperationID = "reattach"

	mutation.ExpectedScopeVersion = 2
	if _, err := mstore.ApplyCanonicalMutation(t.Context(), mutation); err == nil {
		t.Fatal("new operation attached forgotten identity")
	}

	reader := publicTraceReader(t, f.Store)
	if response, err := reader.Trace(t.Context(), traceRequest(mutation)); err == nil || len(response.Sources) != 0 {
		t.Fatalf("trace=%+v err=%v", response, err)
	}

	view, err := reader.LoadCanonicalBoundaryView(t.Context(), mutation.Scope)
	if err != nil || len(view.Snapshot.Atoms) != 0 {
		t.Fatalf("boundary=%+v err=%v", view, err)
	}
}
