package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"reflect"
	"testing"
)

func TestPublicMutationImmutableFamilies(t *testing.T) {
	for _, family := range []string{"source", "message", "revision", "payload"} {
		t.Run(family, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				mutation := nativeMutation(t)
				if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
					t.Fatal(err)
				}

				mutation.ExpectedScopeVersion = 1
				mutation.Operation.OperationID = "changed"
				alterImmutableMutation(&mutation, family)

				before := probe.stamps(t)
				_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
				assertMutationCode(t, err, memory.CodeConflict)

				if !reflect.DeepEqual(before, probe.stamps(t)) {
					t.Fatal("immutable conflict changed records")
				}
			})
		})
	}
}

func alterImmutableMutation(mutation *memory.CanonicalMutation, family string) {
	switch family {
	case "source":
		mutation.Sources[0].Sensitivity = memory.SensitivitySensitive
	case "message":
		mutation.Messages[0].Role = memory.MessageRoleAssistant
	case "revision":
		mutation.Revisions[0].Sensitivity = memory.SensitivitySensitive
	case "payload":
		mutation.Payloads[0].Data = []byte("changed")
		mutation.Payloads[0].Ref = privacyRef(mutation.Payloads[0].Ref.ID, mutation.Payloads[0].Data)
	}
}

func TestPublicMutationUnsignedScopeCAS(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)

		const version = uint64(1<<63) + 7
		probe.write(t, canonicalsql.Key{Family: canonicalsql.ScopeState, Scope: mutation.Scope},
			memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: mutation.Scope, Version: version,
				ChangeSeq: version})
		mutation.ExpectedScopeVersion = version

		outcome, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil || outcome.ScopeVersion != version+1 || outcome.ChangeSeq != version+1 {
			t.Fatalf("unsigned CAS=%+v %v", outcome, err)
		}

		replay, err := store.ApplyCanonicalMutation(t.Context(), mutation)
		if err != nil || replay.ScopeVersion != version+1 {
			t.Fatalf("unsigned replay=%+v %v", replay, err)
		}
	})
}
