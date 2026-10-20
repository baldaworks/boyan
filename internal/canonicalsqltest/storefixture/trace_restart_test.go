//go:build integration

package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicTraceLateSensitiveReopen(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		m := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		later := traceLaterMutation(m)

		later.Revisions[0].Sensitivity = memory.SensitivitySensitive
		if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
			t.Fatal(err)
		}

		if err := f.Store.Close(); err != nil {
			t.Fatal(err)
		}

		reopened, err := f.Open(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()

		got, err := publicTraceReader(t, reopened).Trace(t.Context(), traceRequest(m))
		assertEmptyTrace(t, got, err)
		assertMutationCode(t, err, memory.CodeForgotten)
	})
}
