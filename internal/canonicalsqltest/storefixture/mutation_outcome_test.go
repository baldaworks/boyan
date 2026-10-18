package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicMutationOutcomeBindings(t *testing.T) {
	for _, kind := range []string{"missing-change", "future-scope", "malformed-outcome", "legacy-change",
		"fingerprint-first"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				runPublicOutcomeMutation(t, nil, store, probe, kind)
			})
		})
	}
}

func runPublicOutcomeMutation(t *testing.T, _ *storefixture.Fixture, store mutationStore,
	probe mutationProbe, kind string) {
	t.Helper()

	mutation := nativeMutation(t)
	if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
		t.Fatal(err)
	}

	alterMutationOutcome(t, probe, mutation, kind)

	_, _, readErr := store.LoadCanonicalOperation(t.Context(), mutation.Scope, mutation.Operation.OperationID)
	if kind == "fingerprint-first" {
		mutation.Operation.Fingerprint = "changed"
	}

	_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
	if kind == "legacy-change" {
		if err != nil || readErr != nil {
			t.Fatalf("legacy replay=%v read=%v", err, readErr)
		}

		return
	}

	assertMutationCode(t, readErr, memory.CodeStoreFailure)

	if kind == "fingerprint-first" {
		assertMutationCode(t, err, memory.CodeConflict)
	} else {
		assertMutationCode(t, err, memory.CodeStoreFailure)
	}
}

func alterMutationOutcome(t *testing.T, probe mutationProbe, mutation memory.CanonicalMutation, kind string) {
	t.Helper()

	changeKey := canonicalsql.Key{Family: canonicalsql.ScopeChange, Scope: mutation.Scope,
		Parts: [][]byte{canonicalsql.Uint64(1)}}

	switch kind {
	case "missing-change":
		probe.remove(t, changeKey)
	case "future-scope":
		probe.write(t, canonicalsql.Key{Family: canonicalsql.ScopeState, Scope: mutation.Scope},
			memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: mutation.Scope})
	case "malformed-outcome", "fingerprint-first":
		probe.write(t, canonicalsql.Key{Family: canonicalsql.Operation, Scope: mutation.Scope,
			Parts: [][]byte{[]byte(mutation.Operation.OperationID)}}, map[string]any{"fingerprint": "initial",
			"outcome": map[string]any{"scope_version": 0, "change_seq": 0}})
	case "legacy-change":
		probe.write(t, changeKey, memory.ScopeChange{Sequence: 1, OperationID: mutation.Operation.OperationID,
			OccurredAt: mutation.Operation.CommittedAt, RevisionIDs: []string{"revision"}})
	}
}
