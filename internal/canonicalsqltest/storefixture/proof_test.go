package storefixture_test

import (
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/canonicalsqltest/storefixture"
	memory "github.com/baldaworks/boyan/pkg/memory"
)

func TestPublicProofIdentityContracts(t *testing.T) {
	storefixture.ForEach(t, func(t *testing.T, fixture *storefixture.Fixture) {
		turns, ok := fixture.Store.(memory.CanonicalTurnIdentityStore)
		if !ok {
			t.Fatal("public SQL store lacks turn identity verification")
		}

		boundaries, ok := fixture.Store.(memory.CanonicalBoundaryIdentityStore)
		if !ok {
			t.Fatal("public SQL store lacks boundary identity verification")
		}

		scope := memory.Scope{Key: "test:proof", Kind: memory.ScopeKindPersonal}
		conversation := memory.ConversationRef{ConversationID: "conversation", AgentConversationID: "agent"}
		at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

		turn, err := memory.NewTurn(scope, conversation, "turn", at, "question", "answer")
		if err != nil {
			t.Fatal(err)
		}

		result, err := turns.VerifyCanonicalTurn(t.Context(), turn)
		if err != nil || result.Found || result.FullInputVerified {
			t.Fatalf("new turn: %+v %v", result, err)
		}

		assertPublicNewBoundary(t, boundaries, scope, conversation, at)
	})
}

func assertPublicNewBoundary(t *testing.T, boundaries memory.CanonicalBoundaryIdentityStore,
	scope memory.Scope, conversation memory.ConversationRef, at time.Time) {
	t.Helper()

	boundary, err := memory.NewBoundary(scope, conversation, "close", memory.BoundaryReasonClose, at)
	if err != nil {
		t.Fatal(err)
	}

	proof, err := boundaries.VerifyCanonicalBoundary(t.Context(), boundary)
	if err != nil || proof.Certified || !proof.SafelyNew {
		t.Fatalf("new boundary: %+v %v", proof, err)
	}
}
