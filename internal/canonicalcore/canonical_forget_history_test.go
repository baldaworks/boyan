//go:build integration

package canonicalcore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestScopeForgetNative1025TurnsReturnsCompleteBoundedSummary(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		_, processor, turn, _, store := nativeFlow(t, factory)
		ingestNativeHistory(t, processor, turn, 1024)

		ctx := context.Background()
		active, err := store.ScanActiveMemory(ctx, m.ActiveMemoryScanRequest{Scope: turn.Scope, Limit: 2})
		requireNoError(t, err, "scan active: %v")

		if len(active) != 1 {
			t.Fatalf("active=%d want1", len(active))
		}

		refs := nativeFixturePayloadRefs(t, store)
		adapter, err := forgetops.NewCanonicalForgetAdapter(store)
		requireNoError(t, err, "adapter: %v")

		command := m.ForgetScopeCommand{SchemaVersion: m.DerivedSchemaVersionV1,
			Scope: turn.Scope, RequestID: "large-native-history", ForgottenAt: time.Now().UTC()}
		outcome, err := adapter.ForgetScope(ctx, command)
		requireNoError(t, err, "forget native history: %v")
		requireNoError(t, outcome.Validate(), "summary: %v")

		if outcome.SourceCount != 1025 || outcome.RevisionCount != 1025 || !outcome.ReferencesTruncated {
			t.Fatalf("summary=%+v", outcome)
		}

		if len(outcome.Sources)+len(outcome.Revisions) != 0 {
			t.Fatal("truncated summary includes partial references")
		}

		encoded, err := json.Marshal(outcome)
		requireNoError(t, err, "encode summary: %v")

		const maxSummaryBytes = 1024
		if len(encoded) > maxSummaryBytes {
			t.Fatalf("summary size=%d", len(encoded))
		}

		requireNativePayloadsAbsent(t, store, refs)

		replay, err := adapter.ForgetScope(ctx, command)
		requireNoError(t, err, "replay large forget: %v")

		if replay.ScopeVersion != outcome.ScopeVersion || replay.SourceCount != outcome.SourceCount {
			t.Fatalf("replay differs: %+v", replay)
		}
	})
}

func ingestNativeHistory(t *testing.T, processor *core.CanonicalTurnProcessor, original m.Turn, count int) {
	t.Helper()

	for index := range count {
		next, err := m.NewTurn(original.Scope, original.Conversation, fmt.Sprintf("history-%d", index),
			original.CompletedAt.Add(time.Duration(index+1)*time.Second), "Я живу в Оше", "OK")
		requireNoError(t, err, "history turn: %v")
		_, err = processor.ProcessTurn(context.Background(), next, testProcessorDerivation())
		requireNoError(t, err, "history ingest: %v")
	}
}
