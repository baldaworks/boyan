package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"github.com/baldaworks/boyan/store/archive"
)

func TestPublicReplayAcceptsLegacyChangeWithoutScopeVersion(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		want := preserveOutcomeBaseline(t, store, mutation, true)

		var change memory.ScopeChange
		probe.read(t, mutation.Scope, canonicalsql.ScopeChange, string(canonicalsql.Uint64(want.ChangeSeq)), &change)
		change.ScopeVersion = 0
		probe.write(t, outcomeChangeKey(mutation, want), change)

		got, err := writeBoundary(t, store, mutation, true)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("legacy Commit replay=%+v err=%v want=%+v", got, err, want)
		}
	})
}

func TestPublicReplayClassifiesMalformedStoredOutcomeAsStoreFailure(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)

		want := preserveOutcomeBaseline(t, store, mutation, true)
		if want.ChangeSeq != 1 || want.ScopeVersion != 1 {
			t.Fatalf("fresh outcome=%+v", want)
		}

		alterStoredOutcomeScopeVersion(t, probe, mutation, 0)
		_, err := writeBoundary(t, store, mutation, true)
		requireOutcomePermanentStoreFailure(t, err)
	})
}

func TestPublicReplayRejectsOutcomeOutsideDurableScopeHistory(t *testing.T) {
	for _, route := range []string{"operation", "mutation"} {
		t.Run(route, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, fixture *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				mutation := nativeMutation(t)
				empty := route == "operation"
				preserveOutcomeBaseline(t, store, mutation, empty)

				verifier := fixture.Store.(archive.CanonicalIntegrityVerifier)
				if err := verifier.VerifyCanonicalIntegrity(t.Context()); err != nil {
					t.Fatalf("valid baseline integrity: %v", err)
				}

				alterStoredOutcomeScopeVersion(t, probe, mutation, 999)
				got, err := writeBoundary(t, store, mutation, empty)
				requireOutcomePermanentStoreFailure(t, err)

				if !reflect.DeepEqual(got, memory.CanonicalMutationOutcome{}) {
					t.Fatalf("corrupt replay exposed outcome=%+v", got)
				}

				requireOutcomePermanentStoreFailure(t, verifier.VerifyCanonicalIntegrity(t.Context()))
			})
		})
	}
}

func TestPublicLoadCanonicalOperationRejectsMissingBoundChange(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		outcome := preserveOutcomeBaseline(t, store, mutation, true)
		probe.remove(t, outcomeChangeKey(mutation, outcome))

		_, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, mutation.Operation.OperationID)
		if found {
			t.Fatal("operation with missing bound change reported found")
		}

		requireOutcomePermanentStoreFailure(t, err)
	})
}

func preserveOutcomeBaseline(t *testing.T, store mutationStore, mutation memory.CanonicalMutation,
	empty bool) memory.CanonicalMutationOutcome {
	t.Helper()

	outcome, err := writeBoundary(t, store, mutation, empty)
	if err != nil {
		t.Fatal(err)
	}

	stored, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, mutation.Operation.OperationID)
	if err != nil || !found || stored.Fingerprint != mutation.Operation.Fingerprint ||
		!reflect.DeepEqual(stored.Outcome, outcome) {
		t.Fatalf("valid operation=%+v found=%t err=%v want=%+v", stored, found, err, outcome)
	}

	replayed, err := writeBoundary(t, store, mutation, empty)
	if err != nil || !reflect.DeepEqual(replayed, outcome) {
		t.Fatalf("valid replay=%+v err=%v want=%+v", replayed, err, outcome)
	}

	return outcome
}

func outcomeChangeKey(mutation memory.CanonicalMutation, outcome memory.CanonicalMutationOutcome) canonicalsql.Key {
	return canonicalsql.Key{Family: canonicalsql.ScopeChange, Scope: mutation.Scope,
		Parts: [][]byte{canonicalsql.Uint64(outcome.ChangeSeq)}}
}

func alterStoredOutcomeScopeVersion(t *testing.T, probe mutationProbe, mutation memory.CanonicalMutation,
	version uint64) {
	t.Helper()

	var stored struct {
		Fingerprint string                          `json:"fingerprint"`
		Outcome     memory.CanonicalMutationOutcome `json:"outcome"`
	}
	probe.read(t, mutation.Scope, canonicalsql.Operation, mutation.Operation.OperationID, &stored)
	stored.Outcome.ScopeVersion = version
	probe.write(t, canonicalsql.Key{Family: canonicalsql.Operation, Scope: mutation.Scope,
		Parts: [][]byte{[]byte(mutation.Operation.OperationID)}}, stored)
}

func requireOutcomePermanentStoreFailure(t *testing.T, err error) {
	t.Helper()

	code, class, ok := memory.ClassifyError(err)
	if !ok || code != memory.CodeStoreFailure || class != memory.ErrorClassPermanent {
		t.Fatalf("classification=%s/%s/%t err=%v; want store_failure/permanent", code, class, ok, err)
	}
}
