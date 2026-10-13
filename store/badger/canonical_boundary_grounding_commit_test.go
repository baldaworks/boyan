package badger_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
	b "github.com/baldaworks/boyan/store/badger"
)

func groundingCommitFixture(t *testing.T) (*b.CanonicalStore, m.CanonicalMutation) {
	t.Helper()
	store := openCanonicalTestStore(t, t.TempDir())
	t.Cleanup(func() { closeCanonicalTestStore(t, store) })

	scope := m.Scope{Key: "test:grounding-transaction", Kind: m.ScopeKindPersonal}
	at := time.Date(2026, time.October, 13, 12, 0, 0, 0, time.UTC)
	expiry := at.Add(time.Hour)
	parent := canonicalRevision("parent", "parent-item")
	parent.Temporal.ExpiresAt = &expiry
	parent.Retention = m.RetentionClassEphemeral
	seed := canonicalRevisionMutation(scope, 0, "parent-op", parent)
	seed.Sources = []m.CanonicalSourceRecord{{SourceID: "source-1", Scope: scope,
		Sensitivity: m.SensitivityStandard, Retention: m.RetentionClassStandard,
		Payload: m.PayloadRef{ID: "source-payload", Digest: "source-digest", ByteSize: 3}}}
	seed.Messages = []m.MessageRecord{{MessageID: "message-1", SourceID: "source-1", Role: m.MessageRoleUser,
		Payload: m.PayloadRef{ID: "message-payload", Digest: "message-digest", ByteSize: 3}}}

	_, err := store.ApplyCanonicalMutation(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}

	child := canonicalRevision("child", "child-item")
	child.Temporal = m.Temporal{ObservedAt: at, ExpiresAt: &expiry}
	child.Retention = m.RetentionClassEphemeral
	child.Parents = []string{parent.RevisionID}
	child.GroundingBoundsVersion = 1
	child.GroundingParents = []string{parent.RevisionID}
	mutation := canonicalRevisionMutation(scope, 1, "child-op", child)
	mutation.Operation.CommittedAt = at
	mutation.BoundaryGrounding = &m.BoundaryGroundingCommit{ProcessingAt: at,
		Parents: map[string][]m.RevisionRef{child.RevisionID: {{ItemID: parent.ItemID, RevisionID: parent.RevisionID}}}}

	return store, mutation
}

func TestGroundingCommitRevalidatesBoundsPolicyAndExactParent(t *testing.T) {
	t.Parallel()

	for _, change := range []string{
		"drop_expiry", "refresh_expiry", "downgrade_retention", "wrong_parent_item", "change_time",
	} {
		t.Run(change, func(t *testing.T) {
			store, mutation := groundingCommitFixture(t)
			alterGroundingCommit(&mutation, change)

			before, err := store.LoadScopeState(context.Background(), mutation.Scope)
			if err != nil {
				t.Fatal(err)
			}

			_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
			if _, class, ok := m.ClassifyError(err); !ok || class != m.ErrorClassPermanent {
				t.Fatalf("unsafe proof accepted or untyped error: %v", err)
			}

			assertRejectedGroundingCommit(t, store, mutation, before)
		})
	}
}

func alterGroundingCommit(mutation *m.CanonicalMutation, change string) {
	switch change {
	case "drop_expiry":
		mutation.Revisions[0].Temporal.ExpiresAt = nil
	case "refresh_expiry":
		later := mutation.Revisions[0].Temporal.ExpiresAt.Add(time.Hour)
		mutation.Revisions[0].Temporal.ExpiresAt = &later
	case "downgrade_retention":
		mutation.Revisions[0].Retention = m.RetentionClassStandard
	case "wrong_parent_item":
		mutation.BoundaryGrounding.Parents[mutation.Revisions[0].RevisionID][0].ItemID = "other-item"
	case "change_time":
		mutation.Revisions[0].Temporal.ObservedAt = mutation.Revisions[0].Temporal.ObservedAt.Add(time.Second)
	}
}

func assertRejectedGroundingCommit(t *testing.T, store *b.CanonicalStore,
	mutation m.CanonicalMutation, before m.ScopeState) {
	t.Helper()

	after, err := store.LoadScopeState(context.Background(), mutation.Scope)
	if err != nil {
		t.Fatal(err)
	}

	_, found, err := store.LoadCanonicalOperation(context.Background(), mutation.Scope, mutation.Operation.OperationID)
	if err != nil || before != after || found {
		t.Fatalf("rejected proof changed state/outcome: before=%+v after=%+v found=%v error=%v", before, after, found, err)
	}
}

func TestVerifiedGroundingCommitPersistsProofAndReplaysAfterExpiry(t *testing.T) {
	t.Parallel()
	store, mutation := groundingCommitFixture(t)

	first, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil {
		t.Fatal(err)
	}

	revisions, err := store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
		Scope: mutation.Scope, RevisionIDs: first.RevisionIDs,
	})
	if err != nil || len(revisions) != 1 || revisions[0].GroundingBoundsVersion != 1 ||
		!reflect.DeepEqual(revisions[0].GroundingParents, []string{"parent"}) {
		t.Fatalf("verified proof did not persist: %+v error=%v", revisions, err)
	}

	// Replay must retain the committed outcome even if the caller's fresh
	// processing instant would no longer admit the original direct parent.
	mutation.BoundaryGrounding.ProcessingAt = mutation.BoundaryGrounding.ProcessingAt.Add(2 * time.Hour)

	replayed, err := store.ApplyCanonicalMutation(context.Background(), mutation)
	if err != nil || !reflect.DeepEqual(replayed, first) {
		t.Fatalf("committed replay revalidated expired parents: %+v error=%v", replayed, err)
	}
}

func TestGroundingCommitRejectsSupersededDirectParent(t *testing.T) {
	t.Parallel()
	store, mutation := groundingCommitFixture(t)
	replacement := canonicalRevision("replacement", "parent-item")
	replacement.Revision = 2
	replacement.Parents = []string{"parent"}

	updated, err := store.ApplyCanonicalMutation(context.Background(),
		canonicalRevisionMutation(mutation.Scope, 1, "replace-parent", replacement))
	if err != nil {
		t.Fatal(err)
	}

	mutation.ExpectedScopeVersion = updated.ScopeVersion

	before, err := store.LoadScopeState(context.Background(), mutation.Scope)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeConflict {
		t.Fatalf("superseded direct parent admitted: %v", err)
	}

	assertRejectedGroundingCommit(t, store, mutation, before)
}

func TestGroundingCommitHonorsScopeEraseFence(t *testing.T) {
	t.Parallel()
	store, mutation := groundingCommitFixture(t)

	_, err := store.BeginScopeErase(context.Background(), m.ScopeEraseRequest{
		Scope: mutation.Scope, OperationID: "erase-grounding", Fingerprint: "erase-proof",
		ExpectedScopeVersion: mutation.ExpectedScopeVersion,
		ForgottenAt:          mutation.BoundaryGrounding.ProcessingAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyCanonicalMutation(context.Background(), mutation)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeConflict {
		t.Fatalf("grounding proof bypassed erase fence: %v", err)
	}

	_, found, err := store.LoadCanonicalOperation(context.Background(), mutation.Scope, mutation.Operation.OperationID)
	if err != nil || found {
		t.Fatalf("fenced grounding persisted outcome: found=%v error=%v", found, err)
	}
}
