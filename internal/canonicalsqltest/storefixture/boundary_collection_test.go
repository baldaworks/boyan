package storefixture_test

import (
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
)

func TestPublicBoundaryCollectionBinding(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, p mutationProbe) {
		m := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		p.write(t, canonicalsql.Key{Family: canonicalsql.Head, Scope: m.Scope, Parts: [][]byte{[]byte("alias")}},
			memory.ItemHead{ItemID: "alias", RevisionID: "revision"})

		r, ok := publicTraceReader(t, f.Store).(boundaryAuxReader)
		if !ok {
			t.Fatal("boundary auxiliary ports missing")
		}

		records, err := r.LoadActiveRecallRecords(t.Context(), m.Scope, 10)
		if err == nil || len(records) != 0 {
			t.Fatalf("wrong head hydration=%+v %v", records, err)
		}

		assertMutationCode(t, err, memory.CodeStoreFailure)
	})
}

func TestPublicBoundaryHistoricalCompatibility(t *testing.T) {
	forEachMutation(t, func(t *testing.T, f *storefixture.Fixture, store mutationStore, _ mutationProbe) {
		m := compatibleTraceMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), m); err != nil {
			t.Fatal(err)
		}

		later := traceLaterMutation(m)
		if _, err := store.ApplyCanonicalMutation(t.Context(), later); err != nil {
			t.Fatal(err)
		}

		r, ok := publicTraceReader(t, f.Store).(boundaryAuxReader)
		if !ok {
			t.Fatal("boundary auxiliary ports missing")
		}

		got, found, err := r.LoadCanonicalCompatibility(t.Context(), m.Scope, m.Revisions[0].RevisionID)
		if err != nil || !found || got.Text != "legacy memory" {
			t.Fatalf("historical=%+v %v %v", got, found, err)
		}

		_, found, err = r.LoadCanonicalCompatibility(t.Context(), m.Scope, "missing")
		if err != nil || found {
			t.Fatalf("missing=%v %v", found, err)
		}
	})
}
