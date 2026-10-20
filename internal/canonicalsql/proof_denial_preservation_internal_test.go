//go:build integration

package canonicalsql

import (
	"testing"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestProofTurnMutationAndDenialLinearize(t *testing.T) {
	for _, order := range []string{"denial-first", "mutation-first", "concurrent"} {
		t.Run(order, func(t *testing.T) {
			forEachOwner(t, func(t *testing.T, owner *Owner, reopen func() *Owner) {
				requireProofSuccess(t, owner.PrepareSourcePrivacy(t.Context()))
				h := newPrivacyHistory(t)
				seedPrivacyHistory(t, owner, h)
				mutation := completeProofMutation(t, h)
				err := runTurnDenialOrder(t, owner, h, mutation, order)
				requireProofSuccess(t, owner.Close())
				owner = reopen()
				assertTurnDenialLinearization(t, owner, h, err, order)
			})
		})
	}
}

func runTurnDenialOrder(t *testing.T, owner *Owner, h privacyHistory,
	mutation memory.CanonicalMutation, order string,
) error {
	t.Helper()

	deny := func() error { return owner.DenySource(t.Context(), h.scope, h.source.SourceID, h.turn.CompletedAt) }
	mutate := func() error {
		_, err := owner.ApplyCanonicalMutation(t.Context(), mutation)

		return err
	}

	switch order {
	case "denial-first":
		requireProofSuccess(t, deny())

		return mutate()
	case "mutation-first":
		err := mutate()
		requireProofSuccess(t, err)
		requireProofSuccess(t, deny())

		return err
	default:
		start := make(chan struct{})
		mutated, denied := make(chan error, 1), make(chan error, 1)

		go func() { <-start; mutated <- mutate() }()
		go func() { <-start; denied <- deny() }()

		close(start)
		requireProofSuccess(t, <-denied)

		return <-mutated
	}
}

func assertTurnDenialLinearization(t *testing.T, owner *Owner, h privacyHistory, mutationErr error, order string) {
	t.Helper()

	state, err := owner.LoadScopeState(t.Context(), h.scope)
	requireProofSuccess(t, err)

	proof, err := owner.VerifyCanonicalTurn(t.Context(), h.turn)
	if mutationErr != nil {
		assertCode(t, mutationErr, memory.CodeConflict)
		assertCode(t, err, memory.CodeConflict)

		if state.ChangeSeq != 0 || state.Version != 0 {
			t.Fatalf("denial-first mutation published state: %+v", state)
		}

		return
	}

	assertCommittedTurnDenialState(t, state)

	if order == "denial-first" || err != nil || !proof.Found || !proof.Forgotten || !proof.FullInputVerified {
		t.Fatalf("nonlinear turn/denial: order=%s state=%+v proof=%+v err=%v", order, state, proof, err)
	}
}

func assertCommittedTurnDenialState(t *testing.T, state memory.ScopeState) {
	t.Helper()

	if state.ChangeSeq != 1 || state.Version != 1 {
		t.Fatalf("committed turn/denial state=%+v", state)
	}
}
