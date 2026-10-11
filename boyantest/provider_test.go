package boyantest_test

import boyantest "github.com/baldaworks/boyan/boyantest"

import (
	"context"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestProviderRecordsCallsAndUsesCallbacks(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	conversation := boyan.ConversationRef{ConversationID: "tg-1-0", AgentConversationID: "tg-1-0"}

	turn, err := boyan.NewTurn(scope, conversation, "turn-1", time.Now().UTC(), "hello", "hi")
	requireNoError(t, err, "NewTurn() error = %v")

	boundary, err := boyan.NewBoundary(scope, conversation, "close-1", boyan.BoundaryReasonClose, time.Now().UTC())
	requireNoError(t, err, "NewBoundary() error = %v")

	provider := &boyantest.Provider{}
	requireNoError(t, provider.IngestTurn(context.Background(), turn), "IngestTurn() error = %v")

	requireNoError(t, provider.ApplyBoundary(context.Background(), boundary), "ApplyBoundary() error = %v")

	if len(provider.Turns()) != 1 || len(provider.Boundaries()) != 1 {
		t.Fatalf(
			"recorded calls = turns:%d boundaries:%d",
			len(provider.Turns()),
			len(provider.Boundaries()),
		)
	}

	recorded := provider.Turns()

	recorded[0].Messages[0].Text = "mutated snapshot"
	if got := provider.Turns()[0].Messages[0].Text; got != "hello" {
		t.Fatalf("stored turn text = %q after snapshot mutation, want hello", got)
	}
}
