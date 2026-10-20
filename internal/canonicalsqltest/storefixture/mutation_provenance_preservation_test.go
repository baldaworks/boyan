//go:build integration

package storefixture_test

import (
	"testing"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

type preservedProvenanceEdge struct {
	ChildRevisionID  string `json:"child_revision_id"`
	ParentRevisionID string `json:"parent_revision_id"`
}

func TestPublicCanonicalStorePersistsBidirectionalProvenance(t *testing.T) {
	forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
		first := nativeMutation(t)
		if _, err := store.ApplyCanonicalMutation(t.Context(), first); err != nil {
			t.Fatal(err)
		}

		second := laterMutation(first, "child-revision", 1)

		second.Revisions[0].Parents = []string{first.Revisions[0].RevisionID}
		if _, err := store.ApplyCanonicalMutation(t.Context(), second); err != nil {
			t.Fatal(err)
		}

		parent, child := first.Revisions[0].RevisionID, second.Revisions[0].RevisionID
		forward := readPreservedProvenanceEdge(t, probe, first.Scope, canonicalsql.ProvenanceForward, child, parent)

		reverse := readPreservedProvenanceEdge(t, probe, first.Scope, canonicalsql.ProvenanceReverse, parent, child)
		if forward != reverse || forward.ChildRevisionID != child || forward.ParentRevisionID != parent {
			t.Fatalf("forward=%+v reverse=%+v want child=%q parent=%q", forward, reverse, child, parent)
		}

		source := first.Sources[0].SourceID

		edge := readPreservedProvenanceEdge(t, probe, first.Scope, canonicalsql.SourceRevision, source, parent)
		if edge.ChildRevisionID != parent || edge.ParentRevisionID != source {
			t.Fatalf("source edge=%+v want revision=%q source=%q", edge, parent, source)
		}
	})
}

func readPreservedProvenanceEdge(t *testing.T, probe mutationProbe, scope memory.Scope,
	family canonicalsql.Family, first, second string) preservedProvenanceEdge {
	t.Helper()

	var edge preservedProvenanceEdge

	key := canonicalsql.Key{Family: family, Scope: scope, Parts: [][]byte{[]byte(first), []byte(second)}}

	if err := probe.connection.View(t.Context(), func(tx *canonicalsql.Tx) error {
		encoded, err := tx.Get(t.Context(), key)
		if err != nil {
			return err
		}

		return canonicalsql.Decode(family, encoded, &edge)
	}); err != nil {
		t.Fatal(err)
	}

	return edge
}
