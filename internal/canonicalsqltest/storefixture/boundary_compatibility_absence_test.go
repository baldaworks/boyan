package storefixture_test

import (
	"reflect"
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

// Compatibility absence is a fallback signal
// to lookupOperationRevision, unlike Trace's public forgotten error contract.
func TestPublicBoundaryCompatibilityAbsenceContract(t *testing.T) {
	for _, kind := range []string{"valid", "revision-sensitive", "revision-denied", "source-denied", "missing-source"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
				m := compatibleTraceMutation(t)
				if kind == "revision-sensitive" {
					m.Revisions[0].Sensitivity = memory.SensitivitySensitive
				}

				if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
					t.Fatal(err)
				}

				alterCompatibilityAbsence(t, p, m, kind)
				r := publicTraceReader(t, f.Store).(boundaryAuxReader)

				got, found, err := r.LoadCanonicalCompatibility(t.Context(), m.Scope, m.Revisions[0].RevisionID)
				assertCompatibilityAbsence(t, kind, got, found, err)
			})
		})
	}
}

func assertCompatibilityAbsence(t *testing.T, kind string, got memory.CanonicalCompatibilityPayload,
	found bool, err error) {
	t.Helper()

	if kind == "valid" {
		if err != nil || !found || got.Text != "legacy memory" {
			t.Fatalf("valid control: found=%v text=%q err=%v", found, got.Text, err)
		}

		return
	}

	code, class, _ := memory.ClassifyError(err)
	if found || err != nil || !reflect.DeepEqual(got, memory.CanonicalCompatibilityPayload{}) {
		t.Fatalf("want absence; got found=%v payload=%+v code=%v class=%v err=%v", found, got, code, class, err)
	}
}

func alterCompatibilityAbsence(t *testing.T, p mutationProbe, m memory.CanonicalMutation, kind string) {
	t.Helper()

	switch kind {
	case "revision-denied":
		id := m.Revisions[0].RevisionID
		p.write(t, canonicalsql.Key{Family: canonicalsql.DeniedRevision, Scope: m.Scope,
			Parts: [][]byte{[]byte(id)}}, map[string]any{"revision_id": id, "denied_at": m.Operation.CommittedAt})
	case "source-denied":
		alterReaderAuthority(t, p, m, kind)
	case "missing-source":
		p.remove(t, canonicalsql.Key{Family: canonicalsql.Source, Scope: m.Scope,
			Parts: [][]byte{[]byte(m.Sources[0].SourceID)}})
	}
}
