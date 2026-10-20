package canonicalsql

import (
	"context"
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofLegacyExportKeyRejectsMutation(t *testing.T) {
	for _, denied := range []bool{false, true} {
		name := "retained-source"
		if denied {
			name = "denied-source"
		}

		t.Run(name, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				requireProofSuccess(t, owner.PrepareSourcePrivacy(t.Context()))
				h := newPrivacyHistory(t)
				seedPrivacyHistory(t, owner, h)
				mutation := completeProofMutation(t, h)
				proof, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
				requireProofSuccess(t, err)
				assertLegacyTurnEvidence(t, owner, h, proof)
				err = owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					return alterProofTurnFixture(ctx, tx, h, "legacy-export")
				})
				requireProofSuccess(t, err)

				if denied {
					requireProofSuccess(t, owner.DenySource(t.Context(), h.scope, h.turn.ExportID, h.turn.CompletedAt))
				}

				assertExportKeyMutationRejected(t, owner, h, mutation)

				if denied {
					err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
						key, _ := privacyKey(h.scope, Source, h.turn.ExportID)

						return tx.Delete(ctx, key)
					})
					requireProofSuccess(t, err)
					assertExportKeyMutationRejected(t, owner, h, mutation)
				}
			})
		})
	}
}

func completeProofMutation(t *testing.T, h privacyHistory) memory.CanonicalMutation {
	t.Helper()

	mutation := proofTurnMutation(t, h)
	mutation.SchemaVersion = memory.CanonicalSchemaVersionV1
	mutation.Operation = memory.OperationRecord{OperationID: "new-derivation", Fingerprint: "new-fingerprint",
		CommittedAt: h.turn.CompletedAt}

	return mutation
}

func assertExportKeyMutationRejected(t *testing.T, owner *Owner, h privacyHistory, mutation memory.CanonicalMutation) {
	t.Helper()

	_, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
	assertCode(t, err, memory.CodeConflict)
	_, err = owner.ApplyCanonicalMutation(t.Context(), mutation)
	assertCode(t, err, memory.CodeConflict)
	state, err := owner.LoadScopeState(t.Context(), h.scope)
	requireProofSuccess(t, err)

	if state.ChangeSeq != 0 {
		t.Fatalf("legacy fallback mutated state: %+v", state)
	}
}
