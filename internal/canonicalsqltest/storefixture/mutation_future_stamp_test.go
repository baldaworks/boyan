package storefixture_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// A valid BE8 future stamp must not be blessed by replay or repaired by writes.
func TestPublicMutationRejectsFutureScopeStamp(t *testing.T) {
	for _, delta := range []uint64{1, 100} {
		for _, route := range []string{"read", "replay", "new", "certified-new", "empty-new"} {
			t.Run(fmt.Sprintf("plus-%d/%s", delta, route), func(t *testing.T) {
				forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
					assertFutureScopeStampRejected(t, store, probe, delta, route)
				})
			})
		}
	}
}

type futureStampSnapshot struct {
	clock  []byte
	stamps []canonicalsql.Stamp
}

func readFutureStampSnapshot(t *testing.T, probe mutationProbe) futureStampSnapshot {
	t.Helper()

	clock, err := probe.clock()
	if err != nil {
		t.Fatal(err)
	}
	// Hydrated stamps include the exact original scope/outcome envelopes as well
	// as the corrupt stamp; no checked domain read can mask a changed raw value.
	return futureStampSnapshot{clock: clock, stamps: probe.stamps(t)}
}

func assertFutureScopeStampRejected(t *testing.T, store mutationStore, probe mutationProbe,
	delta uint64, route string) {
	t.Helper()

	mutation := nativeMutation(t)
	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	initial := readFutureStampSnapshot(t, probe)

	clock, err := canonicalsql.DecodeUint64(initial.clock)
	if err != nil {
		t.Fatal(err)
	}

	if err := probe.exec("UPDATE "+probe.table("row_stamp")+" SET last_seq=$1 WHERE family=$2",
		canonicalsql.Uint64(clock+delta), []byte(canonicalsql.ScopeState)); err != nil {
		t.Fatal(err)
	}

	before := readFutureStampSnapshot(t, probe)
	err = callFutureStampRoute(t, store, mutation, route)

	code, _, _ := memory.ClassifyError(err)
	if code != memory.CodeStoreFailure {
		t.Errorf("future stamp route accepted: error=%v", err)
	}

	if !reflect.DeepEqual(before, readFutureStampSnapshot(t, probe)) {
		t.Error("rejected future stamp changed raw clock/stamp/scope/outcome")
	}
}

func callFutureStampRoute(t *testing.T, store mutationStore, mutation memory.CanonicalMutation, route string) error {
	t.Helper()

	switch route {
	case "read":
		_, err := store.LoadScopeState(t.Context(), mutation.Scope)

		return err
	case "replay":
		_, err := store.ApplyCanonicalMutation(t.Context(), mutation)

		return err
	case "empty-new":
		_, err := store.CommitCanonicalOperation(t.Context(), memory.CanonicalOperationCommitRequest{
			Scope: mutation.Scope, ExpectedScopeVersion: 1, OperationID: "new-empty", Fingerprint: "new-empty",
			CommittedAt: mutation.Operation.CommittedAt})

		return err
	}

	later := laterMutation(mutation, "new", 1)
	if route == "certified-new" {
		later.Sources = mutation.Sources
		later.Messages = mutation.Messages
		certifyMutation(t, &later)
	}

	_, err := store.ApplyCanonicalMutation(t.Context(), later)

	return err
}
