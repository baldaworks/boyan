package clienthttp_test

import (
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func mustTurn(t *testing.T, turnID string) boyan.Turn {
	t.Helper()

	turn, err := boyan.NewTurn(
		boyan.Scope{Key: "telegram:client-turn", Kind: boyan.ScopeKindPersonal},
		boyan.ConversationRef{ConversationID: "client-conversation", AgentConversationID: "client-agent"},
		turnID,
		time.Date(2026, time.August, 9, 13, 5, 0, 0, time.UTC),
		"Ada",
		"Ada lives in Bishkek",
	)
	requireNoError(t, err, "NewTurn() error = %v")

	return turn
}
