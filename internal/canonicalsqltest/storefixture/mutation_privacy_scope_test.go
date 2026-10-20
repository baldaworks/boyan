package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestSourcePrivacyExactScopeAndBoundedTouchedSources(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		other := mutation
		other.Scope.Kind = memory.ScopeKindGroup

		other.Sources = append([]memory.CanonicalSourceRecord(nil), mutation.Sources...)
		other.Sources[0].Scope = other.Scope

		other.Items = append([]memory.MemoryItem(nil), mutation.Items...)
		other.Items[0].Scope = other.Scope

		other.Revisions = append([]memory.MemoryRevision(nil), mutation.Revisions...)

		other.Revisions[0].Sensitivity = memory.SensitivitySensitive
		if _, err := store.ApplyCanonicalMutation(t.Context(), other); err != nil {
			t.Fatal(err)
		}

		if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "standard" || privacyState(t,
			probe, other.Scope, other.Sources[0].SourceID) != "sensitive" {
			t.Fatal("scope kind contaminated")
		}

		unrelated := mutation.Sources[0]
		unrelated.SourceID = "unrelated"
		probe.write(t, canonicalsql.Key{Family: canonicalsql.Source, Scope: mutation.Scope,
			Parts: [][]byte{[]byte("unrelated")}}, unrelated)

		if err := probe.exec("UPDATE "+probe.table("source")+" SET envelope=$1 WHERE id=$2",
			[]byte("opaque unrelated"), []byte("unrelated")); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ApplyCanonicalMutation(t.Context(), laterMutation(mutation, "bounded", 1)); err != nil {
			t.Fatalf("scanned unrelated source: %v", err)
		}
	})
}

func TestPrepareSourcePrivacyUnsupportedDanglingEvidenceCannotLaunder(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		mutation := nativeMutation(t)
		historical := mutation.Revisions[0]
		historical.SchemaVersion = "unsupported-history"
		historical.RevisionID = "unsupported-history"
		probe.write(t, canonicalsql.Key{Family: canonicalsql.Revision, Scope: mutation.Scope,
			Parts: [][]byte{[]byte(historical.RevisionID)}}, historical)
		probe.remove(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

		if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if _, err := store.ApplyCanonicalMutation(t.Context(), mutation); err != nil {
			t.Fatal(err)
		}

		if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "unknown" {
			t.Fatal("dangling unsupported history laundered")
		}

		probe.remove(t, canonicalsql.Key{Family: canonicalsql.SourcePrivacyReady})

		if err := store.PrepareSourcePrivacy(t.Context()); err != nil {
			t.Fatal(err)
		}

		if privacyState(t, probe, mutation.Scope, mutation.Sources[0].SourceID) != "unknown" {
			t.Fatal("retry promoted unsupported history")
		}
	})
}

func TestPublicMutationPayloadLocks(t *testing.T) {
	for _, kind := range []string{"locked", "erased"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				mutation := nativeMutation(t)
				id := mutation.Sources[0].Payload.ID
				key := canonicalsql.Key{Family: canonicalsql.EraseBlobLock, Parts: [][]byte{[]byte(id)}}

				var value any = map[string]any{"scope": mutation.Scope, "operation_id": "erase"}

				if kind == "erased" {
					key.Family = canonicalsql.ErasedBlob
					value = true
				}

				probe.write(t, key, value)
				before := probe.snapshot(t)
				_, err := store.ApplyCanonicalMutation(t.Context(), mutation)
				assertMutationCode(t, err, memory.CodeConflict)

				if !reflect.DeepEqual(before, probe.snapshot(t)) {
					t.Fatal("payload gate allowed partial mutation")
				}

				assertMutationState(t, store, mutation.Scope, 0)
			})
		})
	}
}
