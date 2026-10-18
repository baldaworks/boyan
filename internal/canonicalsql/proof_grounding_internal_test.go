package canonicalsql

import (
	"context"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofGroundingConstraints(t *testing.T) {
	for _, kind := range []string{"valid",
		"drop-expiry",
		"extend-expiry",
		"retention",
		"observed",
		"commit-time",
		"wrong-item",
		"superseded",
		"expired",
		"sensitive",
		"denied-source",
		"denied-revision",
		"missing-source",
		"missing-item",
		"wrong-scope",
		"corrupt-version"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) { runProofGroundingCase(t, owner, kind) })
		})
	}
}

func runProofGroundingCase(t *testing.T, owner *Owner, kind string) {
	t.Helper()
	h := newPrivacyHistory(t)
	expiry := h.turn.CompletedAt.Add(time.Hour)
	h.revision.Temporal.ExpiresAt = &expiry

	h.revision.Retention = memory.RetentionClassEphemeral
	if kind == "sensitive" {
		h.revision.Sensitivity = memory.SensitivitySensitive
	}

	if kind == "corrupt-version" {
		h.revision.GroundingBoundsVersion = 2
		h.revision.Parents = []string{"parent"}
		h.revision.GroundingParents = []string{"parent"}
	}

	seedPrivacyHistory(t, owner, h)
	child := h.revision
	child.RevisionID = "child"
	child.ItemID = "child-item"
	child.Parents = []string{h.revision.RevisionID}
	child.GroundingBoundsVersion = 1
	child.GroundingParents = []string{h.revision.RevisionID}
	mutation := memory.CanonicalMutation{Scope: h.scope,
		Revisions: []memory.MemoryRevision{child},
		Operation: memory.OperationRecord{CommittedAt: h.turn.CompletedAt},
		BoundaryGrounding: &memory.BoundaryGroundingCommit{ProcessingAt: h.turn.CompletedAt,
			Parents: map[string][]memory.RevisionRef{"child": {{ItemID: h.item.ItemID,
				RevisionID: h.revision.RevisionID}}}}}

	alterGroundingProof(&mutation, expiry, kind)

	seedProofGroundingHead(t, owner, h, kind)

	err := owner.Update(t.Context(),
		func(ctx context.Context,
			tx *Tx) error {
			return validateCanonicalBoundaryGrounding(ctx,
				tx,
				mutation)
		})
	if kind != "valid" {
		if err == nil {
			t.Fatal("invalid grounding accepted")
		}

		return
	}

	requireProofSuccess(t, err)

	assertResolvedProofBounds(t, owner, h, expiry)
}

func TestProofOutcomeBindings(t *testing.T) {
	for _, kind := range []string{"valid",
		"legacy",
		"missing-change",
		"operation",
		"revision",
		"scope-version",
		"sequence",
		"missing-state",
		"future-state"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				scope := testScope()
				outcome := memory.CanonicalMutationOutcome{ScopeVersion: 2, ChangeSeq: 2, RevisionIDs: []string{"revision"}}
				change := memory.ScopeChange{Sequence: 2,
					ScopeVersion: 2,
					OperationID:  "operation",
					RevisionIDs:  []string{"revision"},
					OccurredAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
				state := memory.ScopeState{SchemaVersion: memory.CanonicalSchemaVersionV1, Scope: scope, Version: 2, ChangeSeq: 2}

				alterOutcomeProof(&change, &state, kind)

				err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					if kind != "missing-change" {
						key, _ := proofChangeKey(scope, 2)
						if err := putPrivacyRecord(ctx, tx, key, ScopeChange, change); err != nil {
							return err
						}
					}

					if kind != "missing-state" {
						key, _ := proofScopeKey(scope)

						return putPrivacyRecord(ctx, tx, key, ScopeState, state)
					}

					return nil
				})
				requireProofSuccess(t, err)

				err = owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
					return validateCanonicalOutcomeBinding(ctx, tx, scope, "operation", outcome)
				})
				if kind == "valid" || kind == "legacy" {
					requireProofSuccess(t, err)
				} else {
					assertCode(t, err, memory.CodeStoreFailure)
				}
			})
		})
	}
}

func alterGroundingProof(mutation *memory.CanonicalMutation, expiry time.Time, kind string) {
	switch kind {
	case "drop-expiry":
		mutation.Revisions[0].Temporal.ExpiresAt = nil
	case "extend-expiry":
		later := expiry.Add(time.Hour)
		mutation.Revisions[0].Temporal.ExpiresAt = &later
	case "retention":
		mutation.Revisions[0].Retention = memory.RetentionClassStandard
	case "observed":
		mutation.Revisions[0].Temporal.ObservedAt = expiry
	case "commit-time":
		mutation.Operation.CommittedAt = expiry
	case "wrong-item":
		mutation.BoundaryGrounding.Parents["child"][0].ItemID = "other-item"
	case "expired":
		mutation.BoundaryGrounding.ProcessingAt = expiry
		mutation.Operation.CommittedAt = expiry
		mutation.Revisions[0].Temporal.ObservedAt = expiry
	case "wrong-scope":
		mutation.Scope.Kind = memory.ScopeKindGroup
	}
}

func seedProofGroundingHead(t *testing.T, owner *Owner, h privacyHistory, kind string) {
	t.Helper()

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		key, _ := privacyKey(h.scope, Head, h.item.ItemID)

		head := memory.ItemHead{ItemID: h.item.ItemID, RevisionID: h.revision.RevisionID}
		if kind == "superseded" {
			head.RevisionID = "replacement"
		}

		if err := putPrivacyRecord(ctx, tx, key, Head, head); err != nil {
			return err
		}

		switch kind {
		case "denied-source":
			key, _ = privacyKey(h.scope, DeniedSource, h.source.SourceID)

			return putPrivacyRecord(ctx,
				tx,
				key,
				DeniedSource,
				deniedSource{SourceID: h.source.SourceID,
					DeniedAt: h.turn.CompletedAt})
		case "denied-revision":
			key, _ = privacyKey(h.scope, DeniedRevision, h.revision.RevisionID)

			return putPrivacyRecord(ctx,
				tx,
				key,
				DeniedRevision,
				deniedRevision{RevisionID: h.revision.RevisionID,
					DeniedAt: h.turn.CompletedAt})
		case "missing-source":
			key, _ = privacyKey(h.scope, Source, h.source.SourceID)

			return tx.Delete(ctx, key)
		case "missing-item":
			key, _ = privacyKey(h.scope, Item, h.item.ItemID)

			return tx.Delete(ctx, key)
		}

		return nil
	})
	requireProofSuccess(t, err)
}

func alterOutcomeProof(change *memory.ScopeChange, state *memory.ScopeState, kind string) {
	switch kind {
	case "legacy":
		change.ScopeVersion = 0
	case "operation":
		change.OperationID = "other"
	case "revision":
		change.RevisionIDs = []string{"other"}
	case "scope-version":
		change.ScopeVersion = 1
	case "sequence":
		change.Sequence = 1
	case "future-state":
		state.Version = 1
		state.ChangeSeq = 1
	}
}

func assertResolvedProofBounds(t *testing.T, owner *Owner, h privacyHistory, expiry time.Time) {
	t.Helper()
	err := owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		records,
			err := resolveBoundaryRecordBounds(ctx,
			tx,
			h.scope,
			[]memory.RecallRecord{{ItemID: h.item.ItemID,
				RevisionID: h.revision.RevisionID}},
			h.turn.CompletedAt)
		if err != nil {
			return err
		}

		if len(records) != 1 ||
			records[0].Temporal.ExpiresAt == nil ||
			!records[0].Temporal.ExpiresAt.Equal(expiry) ||
			records[0].Retention != memory.RetentionClassEphemeral {
			t.Fatalf("lost bounds: %+v", records)
		}

		return nil
	})
	requireProofSuccess(t, err)
}
