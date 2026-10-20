package canonicalsql

import (
	"context"
	"slices"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func assertLegacyTurnEvidence(t *testing.T, owner *Owner, h privacyHistory, legacy memory.CanonicalTurnIdentityResult) {
	t.Helper()

	if legacy.Forgotten || legacy.Source.Payload != h.source.Payload || len(legacy.Messages) != len(h.messages) {
		t.Fatalf("legacy evidence binding: %+v", legacy)
	}

	changed := h.turn
	changed.Messages = slices.Clone(changed.Messages)
	changed.Messages[0].Text += " changed"
	_, err := owner.VerifyCanonicalTurn(t.Context(), changed)
	assertCode(t, err, memory.CodeConflict)
}

func TestProofForgottenCertifiedLineageConflict(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
		h := newPrivacyHistory(t)
		seedPrivacyHistory(t, owner, h)
		mutation := proofTurnMutation(t, h)
		err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if err := persistTurnCertificate(ctx, tx, mutation); err != nil {
				return err
			}

			return alterProofTurnFixture(ctx, tx, h, "certified-denied")
		})
		requireProofSuccess(t, err)
		proof, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
		assertSuccessfulTurnProof(t, proof, err, "certified-denied")

		for _, kind := range []string{"lineage", "previous-conversation"} {
			t.Run(kind, func(t *testing.T) {
				changed := h.turn
				if kind == "lineage" {
					changed.Conversation.LineageID = "changed-lineage"
				} else {
					changed.Conversation.PreviousConversationID = "changed-previous"
				}

				_, err := owner.VerifyCanonicalTurn(t.Context(), changed)
				assertCode(t, err, memory.CodeConflict)
			})
		}
	})
}
