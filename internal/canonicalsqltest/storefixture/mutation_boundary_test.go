package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func mutationBoundary(t *testing.T, scope memory.Scope) *memory.CanonicalBoundaryIdentityCommit {
	t.Helper()

	boundary, err := memory.NewBoundary(scope, memory.ConversationRef{ConversationID: "conversation",
		AgentConversationID: "agent"}, "close", memory.BoundaryReasonClose, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
	if err != nil {
		t.Fatal(err)
	}

	return &identity
}

func writeBoundary(t *testing.T, store mutationStore, mutation memory.CanonicalMutation,
	empty bool) (memory.CanonicalMutationOutcome, error) {
	t.Helper()

	if empty {
		return store.CommitCanonicalOperation(t.Context(),
			memory.CanonicalOperationCommitRequest{Scope: mutation.Scope,
				ExpectedScopeVersion: mutation.ExpectedScopeVersion, OperationID: mutation.Operation.OperationID,
				Fingerprint: mutation.Operation.Fingerprint, CommittedAt: mutation.Operation.CommittedAt,
				BoundaryIdentity: mutation.BoundaryIdentity})
	}

	return store.ApplyCanonicalMutation(t.Context(), mutation)
}

func TestPublicBoundaryWriteRoutes(t *testing.T) {
	for _, route := range []string{"empty", "nonempty"} {
		for _, kind := range []string{"replay", "legacy-replay", "legacy-namespace", "marked-scope",
			"fence-precedence", "competing-inputs", "capability-rollback", "certified-after-history"} {
			t.Run(route+"/"+kind, func(t *testing.T) {
				forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe) {
					runMutationBoundaryCase(t, f, store, probe, route == "empty", kind)
				})
			})
		}
	}
}

func runMutationBoundaryCase(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe,
	empty bool, kind string) {
	t.Helper()
	mutation := nativeMutation(t)
	mutation.BoundaryIdentity = mutationBoundary(t, mutation.Scope)

	switch kind {
	case "legacy-replay":
		legacy := mutation

		legacy.BoundaryIdentity = nil
		if _, err := writeBoundary(t, store, legacy, empty); err != nil {
			t.Fatal(err)
		}

		_, err := writeBoundary(t, store, mutation, empty)
		assertMutationCode(t, err, memory.CodeConflict)
	case "legacy-namespace":
		legacy := mutation
		legacy.BoundaryIdentity = nil

		legacy.Operation.OperationID = "boyan-derived:v1:operation:" + strings.Repeat("!", 64)
		if _, err := writeBoundary(t, store, legacy, empty); err != nil {
			t.Fatal(err)
		}

		mutation.ExpectedScopeVersion = 1
		_, err := writeBoundary(t, store, mutation, empty)
		assertMutationCode(t, err, memory.CodeConflict)
	case "fence-precedence":
		assertBoundaryFencePrecedence(t, store, probe, mutation, empty)
	case "competing-inputs":
		assertBoundaryMutationCompetition(t, store, mutation, empty)
	case "capability-rollback":
		assertBoundaryCapabilityRollback(t, f, store, probe, mutation, empty)
	default:
		assertBoundaryMutationReplay(t, f, store, probe, mutation, empty, kind)
	}
}

func installMutationFence(t *testing.T, probe mutationProbe, scope memory.Scope) {
	t.Helper()

	journal := memory.ScopeEraseJournal{Version: 1, Scope: scope, OperationID: "erase", Fingerprint: "erase",
		ForgottenAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Stage: memory.ScopeEraseReadOutcomes}
	if err := journal.Validate(); err != nil {
		t.Fatal(err)
	}

	probe.write(t, canonicalsql.Key{Family: canonicalsql.EraseJournal, Scope: scope,
		Parts: [][]byte{[]byte("erase")}}, journal)
	probe.write(t, canonicalsql.Key{Family: canonicalsql.EraseFence, Scope: scope,
		Parts: [][]byte{[]byte("pending")}}, map[string]any{"operation_id": "erase"})
}

func assertBoundaryMutationCompetition(t *testing.T, store mutationStore, mutation memory.CanonicalMutation,
	empty bool) {
	t.Helper()

	var group sync.WaitGroup

	results := make(chan error, 2)

	for _, reason := range []memory.BoundaryReason{memory.BoundaryReasonClose, memory.BoundaryReasonReset} {
		boundary := mutation.BoundaryIdentity.Boundary
		boundary.Reason = reason

		identity, err := memory.NewCanonicalBoundaryIdentityCommit(boundary)
		if err != nil {
			t.Fatal(err)
		}

		group.Go(func() {
			attempt := mutation
			attempt.BoundaryIdentity = &identity

			_, err := writeBoundary(t, store, attempt, empty)
			results <- err
		})
	}

	group.Wait()
	close(results)

	wins := 0

	for err := range results {
		if err == nil {
			wins++
		} else {
			assertMutationCode(t, err, memory.CodeConflict)
		}
	}

	if wins != 1 {
		t.Fatalf("identity winners=%d", wins)
	}
}

func assertBoundaryMutationReplay(t *testing.T, f *storefixture.Fixture, store mutationStore,
	probe mutationProbe, mutation memory.CanonicalMutation, empty bool, kind string) {
	t.Helper()

	outcome, err := writeBoundary(t, store, mutation, empty)
	if err != nil {
		t.Fatal(err)
	}

	if kind == "marked-scope" {
		mutation.BoundaryIdentity = nil
		mutation.Operation.OperationID = "boyan-derived:v1:operation:" + strings.Repeat("a", 64)
		mutation.ExpectedScopeVersion = 1
		_, err := writeBoundary(t, store, mutation, empty)
		assertMutationCode(t, err, memory.CodeConflict)

		return
	}

	if kind == "certified-after-history" {
		probe.write(t, canonicalsql.Key{Family: canonicalsql.Operation, Scope: mutation.Scope,
			Parts: [][]byte{[]byte("boyan-derived:v1:operation:" + strings.Repeat("!", 64))}}, true)
	}

	replayed, err := writeBoundary(t, store, mutation, empty)
	if err != nil || !reflect.DeepEqual(outcome, replayed) {
		t.Fatalf("boundary replay %+v %v", replayed, err)
	}

	result, err := f.Store.(memory.CanonicalBoundaryIdentityStore).VerifyCanonicalBoundary(t.Context(),
		mutation.BoundaryIdentity.Boundary)
	if err != nil || !result.Certified {
		t.Fatalf("certificate=%+v %v", result, err)
	}

	changed := mutation.BoundaryIdentity.Boundary
	changed.Reason = memory.BoundaryReasonReset

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(changed)
	if err != nil {
		t.Fatal(err)
	}

	mutation.BoundaryIdentity = &identity
	_, err = writeBoundary(t, store, mutation, empty)
	assertMutationCode(t, err, memory.CodeConflict)
}

func assertBoundaryFencePrecedence(t *testing.T, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation, empty bool) {
	t.Helper()

	if _, err := writeBoundary(t, store, mutation, empty); err != nil {
		t.Fatal(err)
	}

	installMutationFence(t, probe, mutation.Scope)
	changed := mutation.BoundaryIdentity.Boundary
	changed.Reason = memory.BoundaryReasonReset

	identity, err := memory.NewCanonicalBoundaryIdentityCommit(changed)
	if err != nil {
		t.Fatal(err)
	}

	mutation.BoundaryIdentity = &identity
	_, err = writeBoundary(t, store, mutation, empty)

	code, class, _ := memory.ClassifyError(err)
	if code != memory.CodeConflict || class != memory.ErrorClassRetryable {
		t.Fatalf("fence lost precedence: %v", err)
	}
}

func assertBoundaryCapabilityRollback(t *testing.T, f *storefixture.Fixture, store mutationStore, probe mutationProbe,
	mutation memory.CanonicalMutation, empty bool) {
	t.Helper()
	before := probe.stamps(t)
	release := probe.failInserts(t, "operation")
	_, err := writeBoundary(t, store, mutation, empty)

	release()

	if err == nil || !reflect.DeepEqual(before, probe.stamps(t)) {
		t.Fatal("failed boundary published proof/capability")
	}

	assertFailedBoundaryReopensUnreserved(t, f, mutation, empty)
}
