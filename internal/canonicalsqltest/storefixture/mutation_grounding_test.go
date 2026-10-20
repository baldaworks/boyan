package storefixture_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicMutationGrounding(t *testing.T) {
	for _, kind := range []string{"valid-expired-replay", "drop-expiry", "retention", "wrong-parent-item",
		"superseded", "fence"} {
		t.Run(kind, func(t *testing.T) {
			forEachMutation(t, func(t *testing.T, _ *storefixture.Fixture, store mutationStore, probe mutationProbe) {
				runPublicGroundingMutation(t, nil, store, probe, kind)
			})
		})
	}
}

func runPublicGroundingMutation(t *testing.T, _ *storefixture.Fixture, store mutationStore,
	probe mutationProbe, kind string) {
	t.Helper()
	parent := nativeMutation(t)
	expiry := parent.Operation.CommittedAt.Add(time.Hour)
	parent.Revisions[0].Temporal.ExpiresAt = &expiry

	parent.Revisions[0].Retention = memory.RetentionClassEphemeral
	if _, err := store.ApplyCanonicalMutation(t.Context(), parent); err != nil {
		t.Fatal(err)
	}

	child := laterMutation(parent, "child", 1)
	child.Revisions[0].Parents = []string{"revision"}
	child.Revisions[0].GroundingBoundsVersion = 1
	child.Revisions[0].GroundingParents = []string{"revision"}
	child.Revisions[0].ItemID = "child-item"
	child.Items = []memory.MemoryItem{{ItemID: "child-item", Scope: parent.Scope,
		Kind: memory.MemoryKindState, MemoryKey: "child-key"}}
	child.Heads = []memory.ItemHead{{ItemID: "child-item", RevisionID: "child"}}
	child.BoundaryGrounding = &memory.BoundaryGroundingCommit{ProcessingAt: parent.Operation.CommittedAt,
		Parents: map[string][]memory.RevisionRef{"child": {{ItemID: "item", RevisionID: "revision"}}}}

	alterGroundingMutation(t, store, probe, parent, &child, kind)

	before := probe.snapshot(t)

	outcome, err := store.ApplyCanonicalMutation(t.Context(), child)
	if kind != "valid-expired-replay" {
		if err == nil {
			t.Fatal("invalid grounding committed")
		}

		if !reflect.DeepEqual(before, probe.snapshot(t)) {
			t.Fatal("invalid grounding modified rows")
		}

		return
	}

	if err != nil {
		t.Fatal(err)
	}

	assertGroundingMutationReplay(t, store, probe, child, outcome, expiry)
}

func alterGroundingMutation(t *testing.T, store mutationStore, probe mutationProbe,
	parent memory.CanonicalMutation, child *memory.CanonicalMutation, kind string) {
	t.Helper()

	switch kind {
	case "drop-expiry":
		child.Revisions[0].Temporal.ExpiresAt = nil
	case "retention":
		child.Revisions[0].Retention = memory.RetentionClassStandard
	case "wrong-parent-item":
		child.BoundaryGrounding.Parents["child"][0].ItemID = "other"
	case "superseded":
		replacement := laterMutation(parent, "replacement", 1)
		if _, err := store.ApplyCanonicalMutation(t.Context(), replacement); err != nil {
			t.Fatal(err)
		}

		child.ExpectedScopeVersion = 2
	case "fence":
		installMutationFence(t, probe, parent.Scope)
	}
}

func assertGroundingMutationReplay(t *testing.T, store mutationStore, probe mutationProbe,
	child memory.CanonicalMutation, outcome memory.CanonicalMutationOutcome, expiry time.Time) {
	t.Helper()

	var revision memory.MemoryRevision
	probe.read(t, child.Scope, canonicalsql.Revision, "child", &revision)

	if revision.GroundingBoundsVersion != 1 || revision.Temporal.ExpiresAt == nil ||
		!revision.Temporal.ExpiresAt.Equal(expiry) {
		t.Fatalf("lost proof=%+v", revision)
	}

	child.BoundaryGrounding.ProcessingAt = expiry.Add(time.Hour)
	child.Operation.CommittedAt = child.BoundaryGrounding.ProcessingAt

	replay, err := store.ApplyCanonicalMutation(t.Context(), child)
	if err != nil || !reflect.DeepEqual(outcome, replay) {
		t.Fatalf("expired committed replay=%+v %v", replay, err)
	}
}
