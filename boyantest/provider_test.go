package boyantest

import (
	"context"
	"testing"
	"time"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

func TestProviderRecordsCallsAndUsesCallbacks(t *testing.T) {
	t.Parallel()

	scope := boyan.Scope{Key: "telegram:1:0", Kind: boyan.ScopeKindPersonal}
	session := boyan.SessionRef{SessionID: "tg-1-0", AgentSessionID: "tg-1-0"}
	turn, err := boyan.NewTurn(scope, session, "turn-1", time.Now().UTC(), "hello", "hi")
	if err != nil {
		t.Fatalf("NewTurn() error = %v", err)
	}
	boundary, err := boyan.NewBoundary(scope, session, "close-1", boyan.BoundaryReasonClose, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewBoundary() error = %v", err)
	}
	provider := &Provider{}
	if err := provider.IngestTurn(context.Background(), turn); err != nil {
		t.Fatalf("IngestTurn() error = %v", err)
	}
	if err := provider.ApplyBoundary(context.Background(), boundary); err != nil {
		t.Fatalf("ApplyBoundary() error = %v", err)
	}
	if len(provider.Turns()) != 1 || len(provider.Boundaries()) != 1 {
		t.Fatalf("recorded calls = turns:%d boundaries:%d", len(provider.Turns()), len(provider.Boundaries()))
	}
	recorded := provider.Turns()
	recorded[0].Messages[0].Text = "mutated snapshot"
	if got := provider.Turns()[0].Messages[0].Text; got != "hello" {
		t.Fatalf("stored turn text = %q after snapshot mutation, want hello", got)
	}
}
