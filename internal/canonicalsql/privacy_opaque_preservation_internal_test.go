package canonicalsql

import (
	"context"
	"fmt"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPrivacyOpaqueHistoryTaintsExactScopeAndFreshSource(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := prepareTraceHistory(t, owner)
		other := h
		other.scope.Kind = memory.ScopeKindGroup
		other.source.Scope = other.scope
		other.item.Scope = other.scope
		seedPrivacyHistory(t, owner, other)

		err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			key, _ := privacyKey(h.scope, Revision, "opaque-history")
			// SQL rows bind revision identity independently of the envelope. Keep
			// that binding valid while the unsupported history has no attributable
			// evidence, preserving the old opaque-history classification boundary.
			opaque := h.revision
			opaque.RevisionID, opaque.SchemaVersion = "opaque-history", "unsupported"

			opaque.Evidence = nil
			if err := putPrivacyRecord(ctx, tx, key, Revision, opaque); err != nil {
				return err
			}

			return tx.Delete(ctx, Key{Family: SourcePrivacyReady})
		})
		requireEraseTestNoError(t, err)
		requireEraseTestNoError(t, owner.PrepareSourcePrivacy(t.Context()))
		assertPrivacyHistory(t, owner, h, "unknown")
		assertPrivacyHistory(t, owner, other, "standard")

		fresh := freshPrivacySubset(h)
		_, err = owner.ApplyCanonicalMutation(t.Context(), fresh)
		requireEraseTestNoError(t, err)

		h.source = fresh.Sources[0]
		assertPrivacyHistory(t, owner, h, "unknown")
	})
}

func freshPrivacySubset(h privacyHistory) memory.CanonicalMutation {
	fresh := historyMutation(h)
	fresh.ExpectedScopeVersion = 1
	fresh.Operation.OperationID, fresh.Operation.Fingerprint = "fresh", "fresh"
	fresh.Sources = append([]memory.CanonicalSourceRecord(nil), fresh.Sources...)
	fresh.Sources[0].SourceID = "fresh-source"
	fresh.Messages = append([]memory.MessageRecord(nil), fresh.Messages...)

	for i := range fresh.Messages {
		fresh.Messages[i].SourceID = "fresh-source"
		fresh.Messages[i].MessageID = fmt.Sprintf("fresh-%d", i)
	}

	fresh.Revisions = append([]memory.MemoryRevision(nil), fresh.Revisions...)
	fresh.Revisions[0].RevisionID = "fresh-revision"
	fresh.Revisions[0].Evidence = append([]memory.EvidenceRef(nil), fresh.Revisions[0].Evidence...)
	fresh.Revisions[0].Evidence[0].SourceID = "fresh-source"
	fresh.Revisions[0].Evidence[0].MessageID = fresh.Messages[0].MessageID
	fresh.Heads = []memory.ItemHead{{ItemID: fresh.Items[0].ItemID, RevisionID: "fresh-revision"}}

	return fresh
}
