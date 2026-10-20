package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicMutationRejectsCorruptScopeAndStamp(t *testing.T) {
	for _, kind := range []string{"scope-column"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				runMutationScopeCorruption(t, store, probe, kind)
			})
		})
	}
}

func runMutationScopeCorruption(t *testing.T, store mutationStore, probe mutationProbe, kind string) {
	t.Helper()

	mutation := nativeMutation(t)
	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	before := probe.snapshot(t)
	alterMutationScopeCorruption(t, probe, kind, true)

	later := laterMutation(mutation, "must-not-commit", 1)
	_, err := store.ApplyCanonicalMutation(t.Context(), later)
	assertMutationCode(t, err, memory.CodeStoreFailure)
	alterMutationScopeCorruption(t, probe, kind, false)
	assertMutationState(t, store, mutation.Scope, 1)

	if !reflect.DeepEqual(before, probe.snapshot(t)) {
		t.Fatal("failed corruption attempt changed stamps")
	}

	_, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, later.Operation.OperationID)
	if err != nil || found {
		t.Fatalf("partial operation=%v %v", found, err)
	}
}

func alterMutationScopeCorruption(t *testing.T, probe mutationProbe, kind string, corrupt bool) {
	t.Helper()

	var err error

	if kind == "scope-column" {
		version := uint64(1)
		if corrupt {
			version = 7
		}

		err = probe.exec("UPDATE "+probe.table("scope_state")+" SET version_u64=$1", canonicalsql.Uint64(version))
	}

	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicMutationPersistsDeliveryBinding(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		mutation.Lifecycle = []memory.LifecycleEvent{{EventID: "activate", RevisionID: "revision",
			Type: memory.LifecycleEventActivate, OccurredAt: mutation.Operation.CommittedAt}}

		mutation.Delivery = []memory.DeliveryOutboxRecord{{DeliveryID: "pending",
			OperationID: mutation.Operation.OperationID, PayloadRef: mutation.Revisions[0].Payload,
			CreatedAt: mutation.Operation.CommittedAt}}
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		var delivery memory.DeliveryOutboxRecord
		probe.read(t, mutation.Scope, canonicalsql.Delivery, "pending", &delivery)

		if delivery.OperationID != "initial" {
			t.Fatal("pending outbox is not bound to operation")
		}
	})
}
