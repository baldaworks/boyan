package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicMutationRejectsCorruptScope(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		before := probe.snapshot(t)
		if err := probe.exec("UPDATE "+probe.table("scope_state")+" SET version_u64=$1", canonicalsql.Uint64(7)); err != nil {
			t.Fatal(err)
		}

		later := laterMutation(mutation, "must-not-commit", 1)
		_, err := store.ApplyCanonicalMutation(t.Context(), later)
		assertMutationCode(t, err, memory.CodeStoreFailure)

		if err := probe.exec("UPDATE "+probe.table("scope_state")+" SET version_u64=$1", canonicalsql.Uint64(1)); err != nil {
			t.Fatal(err)
		}

		assertMutationState(t, store, mutation.Scope, 1)

		if !reflect.DeepEqual(before, probe.snapshot(t)) {
			t.Fatal("failed corruption attempt changed canonical rows")
		}

		_, found, err := store.LoadCanonicalOperation(t.Context(), mutation.Scope, later.Operation.OperationID)
		if err != nil || found {
			t.Fatalf("partial operation=%v %v", found, err)
		}
	})
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
