package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"testing"
)

func TestPublicMutationRejectsCorruptScopeAndStamp(t *testing.T) {
	for _, kind := range []string{"scope-column", "stamp"} {
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

	before := probe.stamps(t)
	alterMutationScopeCorruption(t, probe, kind, true)

	later := laterMutation(mutation, "must-not-commit", 1)
	_, err := store.ApplyCanonicalMutation(t.Context(), later)
	assertMutationCode(t, err, memory.CodeStoreFailure)
	alterMutationScopeCorruption(t, probe, kind, false)
	assertMutationState(t, store, mutation.Scope, 1)

	if !reflect.DeepEqual(before, probe.stamps(t)) {
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
	} else {
		err = probe.exec("UPDATE "+probe.table("row_stamp")+" SET deleted=$1 WHERE family=$2", corrupt,
			[]byte(canonicalsql.ScopeState))
	}

	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicMutationRecordsShareStamp(t *testing.T) {
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

		stamps := probe.stamps(t)

		var sequence uint64

		families := make(map[canonicalsql.Family]bool)

		for _, stamp := range stamps {
			if stamp.Key.Family == canonicalsql.SourcePrivacyReady {
				continue
			}

			if sequence == 0 {
				sequence = stamp.Cursor.Sequence
			}

			if stamp.Cursor.Sequence != sequence {
				t.Fatal("mutation family escaped atomic clock")
			}

			families[stamp.Key.Family] = true
		}

		for _, family := range []canonicalsql.Family{canonicalsql.Source, canonicalsql.Message, canonicalsql.Item,
			canonicalsql.Revision, canonicalsql.Payload, canonicalsql.Head, canonicalsql.Lifecycle,
			canonicalsql.Delivery, canonicalsql.SourceRevision, canonicalsql.SourcePrivacy, canonicalsql.ScopeState,
			canonicalsql.ScopeChange, canonicalsql.Operation} {
			if !families[family] {
				t.Fatalf("missing mutation stamp: %s", family)
			}
		}

		var delivery memory.DeliveryOutboxRecord
		probe.read(t, mutation.Scope, canonicalsql.Delivery, "pending", &delivery)

		if delivery.OperationID != "initial" {
			t.Fatal("pending outbox is not bound to operation")
		}
	})
}
