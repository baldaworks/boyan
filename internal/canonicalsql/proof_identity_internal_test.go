package canonicalsql

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func proofBoundary(t *testing.T, scope memory.Scope) memory.CanonicalBoundaryIdentityCommit {
	t.Helper()

	b,
		err := memory.NewBoundary(scope,
		memory.ConversationRef{ConversationID: "conversation",
			AgentConversationID: "agent"},
		"close",
		memory.BoundaryReasonClose,
		time.Date(2026,
			1,
			1,
			0,
			0,
			0,
			0,
			time.UTC))
	requireProofSuccess(t, err)

	id, err := memory.NewCanonicalBoundaryIdentityCommit(b)
	requireProofSuccess(t, err)

	return id
}

func proofTurnMutation(t *testing.T, h privacyHistory) memory.CanonicalMutation {
	t.Helper()

	fingerprint, err := memory.CanonicalTurnFingerprint(h.turn)
	requireProofSuccess(t, err)

	ids := make([]string, 0, len(h.turn.Messages))
	for _, message := range h.turn.Messages {
		ids = append(ids, message.MessageID)
	}

	return memory.CanonicalMutation{Scope: h.scope,
		Sources:  []memory.CanonicalSourceRecord{h.source},
		Messages: h.messages,
		Payloads: h.payloads,
		TurnIdentity: &memory.CanonicalTurnIdentityCommit{Turn: h.turn,
			Certificate: memory.CanonicalTurnCertificate{Version: 1,
				SourceID:    h.turn.SourceID,
				Fingerprint: fingerprint,
				MessageIDs:  ids}}}
}

func TestProofTurnPersistence(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		h := newPrivacyHistory(t)

		mutation := proofTurnMutation(t, h)
		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			_, err := prepareTurnIdentityMutation(ctx, tx, mutation)

			return err
		}))

		seedPrivacyHistory(t, owner, h)

		legacy, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
		if err != nil || !legacy.Found || legacy.FullInputVerified {
			t.Fatalf("legacy proof: %+v %v", legacy, err)
		}

		assertLegacyTurnEvidence(t, owner, h, legacy)

		assertTurnProofRollback(t, owner, h, mutation)

		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			if _, err := prepareTurnIdentityMutation(ctx, tx, mutation); err != nil {
				return err
			}

			return persistTurnCertificate(ctx, tx, mutation)
		}))

		requireProofSuccess(t, owner.Close())

		owner = reopen()

		result, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
		if err != nil || !result.FullInputVerified || !slices.Equal(result.Messages, h.messages) {
			t.Fatalf("certified proof: %+v %v", result, err)
		}

		changed := h.turn
		changed.Messages = slices.Clone(changed.Messages)
		changed.Messages[0].Text += " changed"
		_, err = owner.VerifyCanonicalTurn(t.Context(), changed)
		assertCode(t, err, memory.CodeConflict)

		mutation.TurnIdentity.Certificate.Fingerprint = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		err = owner.Update(t.Context(),
			func(ctx context.Context,
				tx *Tx) error {
				return persistTurnCertificate(ctx,
					tx,
					mutation)
			})
		assertCode(t, err, memory.CodeConflict)
	})
}

func TestProofBoundaryPersistence(t *testing.T) {
	forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
		identity := proofBoundary(t, testScope())
		assertBoundaryProofRollback(t, owner, identity)

		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return persistBoundaryCertificate(ctx, tx, identity.Boundary.Scope, &identity)
		}))

		requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
			return persistBoundaryCertificate(ctx, tx, identity.Boundary.Scope, &identity)
		}))

		requireProofSuccess(t, owner.Close())

		owner = reopen()

		result, err := owner.VerifyCanonicalBoundary(t.Context(), identity.Boundary)
		if err != nil || !result.Certified {
			t.Fatalf("lost certificate: %+v %v", result, err)
		}

		changed := identity.Boundary
		changed.Reason = memory.BoundaryReasonReset
		_, err = owner.VerifyCanonicalBoundary(t.Context(), changed)
		assertCode(t, err, memory.CodeConflict)
		other := proofBoundary(t, memory.Scope{Key: identity.Boundary.Scope.Key, Kind: memory.ScopeKindGroup})

		result, err = owner.VerifyCanonicalBoundary(t.Context(), other.Boundary)
		if err != nil || result.Certified || !result.SafelyNew {
			t.Fatalf("scope mixed: %+v %v", result, err)
		}
	})
}

func TestProofBoundaryRawCorruption(t *testing.T) {
	for _, kind := range []string{"unknown-field", "oversize", "marker-unknown"} {
		t.Run(kind, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, _ func() *Owner) {
				identity := proofBoundary(t, testScope())
				family, id := BoundaryCertificate, identity.Certificate.ExportID

				value := map[string]any{"version": 1,
					"export_id":   id,
					"fingerprint": identity.Certificate.Fingerprint,
					"private":     "forbidden"}
				if kind == "oversize" {
					value["private"] = string(make([]byte, 600))
				}

				if kind == "marker-unknown" {
					family, id = BoundaryCapability, "v1"
					value = map[string]any{"version": 1, "scope": identity.Boundary.Scope, "private": "forbidden"}
				}

				requireProofSuccess(t, owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
					key, _ := privacyKey(identity.Boundary.Scope, family, id)

					return tx.Put(ctx, key, envelope(t, family, value))
				}))

				_, err := owner.VerifyCanonicalBoundary(t.Context(), identity.Boundary)
				if err == nil {
					t.Fatal("raw corrupt proof authorized identity")
				}
			})
		})
	}
}

func assertTurnProofRollback(t *testing.T, owner *Owner, h privacyHistory, mutation memory.CanonicalMutation) {
	t.Helper()

	abort := errors.New("abort proof fixture")

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if err := persistTurnCertificate(ctx, tx, mutation); err != nil {
			return err
		}

		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}

	requireProofSuccess(t, owner.View(t.Context(), func(ctx context.Context, tx *Tx) error {
		_, found, err := loadTurnCertificate(ctx, tx, h.scope, h.source.SourceID)
		if found {
			t.Fatal("rolled back certificate survived")
		}

		return err
	}))
}

func assertBoundaryProofRollback(t *testing.T, owner *Owner, identity memory.CanonicalBoundaryIdentityCommit) {
	t.Helper()

	opKey, _ := privacyKey(identity.Boundary.Scope, Operation, "operation")
	abort := errors.New("rollback")

	err := owner.Update(t.Context(), func(ctx context.Context, tx *Tx) error {
		if err := prepareBoundaryIdentity(ctx, tx, identity.Boundary.Scope, &identity, "operation", opKey); err != nil {
			return err
		}

		if err := persistBoundaryCertificate(ctx, tx, identity.Boundary.Scope, &identity); err != nil {
			return err
		}

		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
}

func requireProofSuccess(t *testing.T, err error) {
	t.Helper()

	if err != nil {
		t.Fatal(err)
	}
}
