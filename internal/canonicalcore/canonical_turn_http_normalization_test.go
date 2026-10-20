//go:build integration

package canonicalcore_test

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestAudit5HTTPNormalizedTurnReplay(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		testHTTPNormalizedTurnReplay(t, "offset", factory)
	})
}

func TestHTTPNormalizedTurnReplayUsesCanonicalAuthority(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, kind := range []string{"status", "source", "messages", "combined-restart"} {
			t.Run(kind, func(t *testing.T) { testHTTPNormalizedTurnReplay(t, kind, factory) })
		}
	})
}

func testHTTPNormalizedTurnReplay(t *testing.T, kind string, factory coreFactory) {
	t.Helper()

	f := newTurnReceiptFixture(t, factory)
	if response := f.post(t, f.turn); response.Code != http.StatusOK {
		t.Fatalf("initial: %s", response.Body)
	}

	original := f.record(t)
	operation := receiptCanonicalOperation(t, f, original.Outcome.OperationIDs[0])
	before, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "before: %v")

	retry := equivalentHTTPReceiptTurn(f.turn, kind)
	if kind == "combined-restart" {
		f.close(t)
		f.open(t)
	}

	response := f.post(t, retry)
	if response.Code != http.StatusOK {
		t.Fatalf("equivalent replay: %d %s", response.Code, response.Body)
	}

	assertTurnReceiptReplayUnchanged(t, f, original, before, response)

	fingerprint, err := m.TurnIngestFingerprint(m.IngestEnvelope[m.Turn]{SchemaVersion: m.IngestHTTPSchemaVersionV1,
		ProducerID: "old-producer", ScopeSequence: 1, Payload: retry})
	requireNoError(t, err, "equivalent hash: %v")

	if original.Fingerprint != fingerprint {
		t.Fatal("equivalent HTTP retry still has another delivery identity")
	}

	if !reflect.DeepEqual(operation, receiptCanonicalOperation(t, f, original.Outcome.OperationIDs[0])) {
		t.Fatal("normalized retry changed revision IDs or canonical operation")
	}
}

func equivalentHTTPReceiptTurn(turn m.Turn, kind string) m.Turn {
	switch kind {
	case "offset":
		turn.CompletedAt = turn.CompletedAt.In(time.FixedZone("offset", 6*60*60))
	case "status":
		turn.TerminalStatus = ""
	case "source":
		turn.SourceID = ""
	case "messages":
		turn.Messages = append([]m.Message(nil), turn.Messages...)
		for i := range turn.Messages {
			turn.Messages[i].MessageID = ""
		}
	case "combined-restart":
		return legacyReceiptWireTurn(turn)
	}

	return turn
}

func TestHTTPChangedTurnRemainsRejected(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		changes := map[string]func(*m.Turn){
			"text":         func(turn *m.Turn) { turn.Messages[0].Text = "changed" },
			"role":         func(turn *m.Turn) { turn.Messages[0].Role = m.MessageRoleAssistant },
			"order":        func(turn *m.Turn) { turn.Messages[0], turn.Messages[1] = turn.Messages[1], turn.Messages[0] },
			"instant":      func(turn *m.Turn) { turn.CompletedAt = turn.CompletedAt.Add(time.Nanosecond) },
			"status":       func(turn *m.Turn) { turn.TerminalStatus = m.TurnTerminalStatusFailed },
			"conversation": func(turn *m.Turn) { turn.Conversation.ConversationID = "different" },
			"lineage":      func(turn *m.Turn) { turn.Conversation.LineageID = "different" },
			"previous":     func(turn *m.Turn) { turn.Conversation.PreviousConversationID = "different" },
			"agent":        func(turn *m.Turn) { turn.Conversation.AgentConversationID = "different" },
		}
		for name, change := range changes {
			t.Run(name, func(t *testing.T) { testHTTPChangedTurn(t, change, factory) })
		}
	})
}

func testHTTPChangedTurn(t *testing.T, change func(*m.Turn), factory coreFactory) {
	t.Helper()

	f := newTurnReceiptFixture(t, factory)
	if response := f.post(t, f.turn); response.Code != http.StatusOK {
		t.Fatalf("initial: %s", response.Body)
	}

	original := f.record(t)
	before, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "before: %v")

	changed := f.turn
	changed.Messages = append([]m.Message(nil), changed.Messages...)
	change(&changed)

	want := http.StatusConflict
	if changed.Validate() != nil {
		want = http.StatusBadRequest
	}

	response := f.post(t, changed)
	if response.Code != want {
		t.Fatalf("changed retry: %d %s", response.Code, response.Body)
	}

	after, err := f.store.LoadScopeState(context.Background(), f.turn.Scope)
	requireNoError(t, err, "after: %v")

	if before != after || !reflect.DeepEqual(original, f.record(t)) || f.calls != 1 || f.processCalls != 1 {
		t.Fatal("changed retry repeated effects or changed the original outcome")
	}
}

func TestHTTPExactScopeKindsKeepIndependentReceiptLanes(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		f := newTurnReceiptFixture(t, factory)
		if response := f.post(t, f.turn); response.Code != http.StatusOK {
			t.Fatalf("personal: %s", response.Body)
		}

		original := f.record(t)
		other := f.turn

		other.Scope.Kind = m.ScopeKindGroup
		if response := f.post(t, other); response.Code != http.StatusOK {
			t.Fatalf("group: %d %s", response.Code, response.Body)
		}

		second, found, err := f.store.LoadIngestSequence(context.Background(), "old-producer", other.Scope, 1)
		requireNoError(t, err, "group receipt: %v")

		if !found || original.Fingerprint == second.Fingerprint || second.Outcome.Scope != other.Scope ||
			!reflect.DeepEqual(original, f.record(t)) || f.calls != 2 || f.processCalls != 2 {
			t.Fatal("exact scope lanes merged")
		}
	})
}
