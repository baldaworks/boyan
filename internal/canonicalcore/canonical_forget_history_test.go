//go:build integration

package canonicalcore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

//nolint:funlen // Summary, replay and projection checkpoints share one genuine 1025-turn history.
func TestScopeForgetNative1025TurnsSummaryProjectionAndReplay(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, processor, turn, _, store := nativeFlow(t, factory)
		ingestNativeHistory(t, processor, turn, 1024)

		ctx := context.Background()
		active, err := store.ScanActiveMemory(ctx, m.ActiveMemoryScanRequest{Scope: turn.Scope, Limit: 2})
		requireNoError(t, err, "scan active: %v")

		if len(active) != 1 {
			t.Fatalf("active=%d want1", len(active))
		}

		refs := nativeFixturePayloadRefs(t, store)
		t.Log("checkpoint: genuine 1025-turn history, one active item and payload references captured")

		path := filepath.Join(t.TempDir(), "index")
		idx, runtime := openLargeForgetProjection(t, path, store, reader)
		requireNoError(t, runtime.Sync(ctx, turn.Scope), "initial sync: %v")
		assertLargeForgetIndex(t, idx, turn.Scope, 0, "")
		t.Log("checkpoint: initial projection synced with one physical hit")

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
		t.Logf("checkpoint: complete bounded summary sources=%d revisions=%d bytes=%d; original payloads absent",
			outcome.SourceCount, outcome.RevisionCount, len(encoded))

		replay, err := adapter.ForgetScope(ctx, command)
		requireNoError(t, err, "replay large forget: %v")

		if replay.ScopeVersion != outcome.ScopeVersion || replay.SourceCount != outcome.SourceCount {
			t.Fatalf("replay differs: %+v", replay)
		}

		t.Log("checkpoint: immediate identical-command replay preserves version and source count")

		next, err := m.NewTurn(turn.Scope, turn.Conversation, "after-large-forget", turn.CompletedAt.Add(2*time.Hour),
			"Я живу в Казани", "OK")
		requireNoError(t, err, "later turn: %v")
		later, err := processor.ProcessTurn(ctx, next, testProcessorDerivation())
		requireNoError(t, err, "later ingest: %v")
		requireNoError(t, runtime.Sync(ctx, turn.Scope), "bounded invalidation rebuild: %v")
		assertLargeForgetIndex(t, idx, turn.Scope, later.ChangeSeq, later.RevisionIDs[0])
		t.Log("checkpoint: live projection contains exactly the fresh later revision")
		requireNoError(t, runtime.Close(ctx), "runtime close: %v")
		requireNoError(t, idx.Close(), "index close: %v")

		idx, runtime = openLargeForgetProjection(t, path, store, reader)
		requireNoError(t, runtime.Start(ctx), "projection restart: %v")
		replay, err = adapter.ForgetScope(ctx, command)
		requireNoError(t, err, "old erase replay: %v")

		if replay.ScopeVersion != outcome.ScopeVersion {
			t.Fatal("replay changed outcome")
		}

		requireNoError(t, runtime.Sync(ctx, turn.Scope), "replay sync: %v")
		assertLargeForgetIndex(t, idx, turn.Scope, later.ChangeSeq, later.RevisionIDs[0])
		t.Log("checkpoint: same-path projection reopened against live canonical owner; original replay retains fresh later hit")
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
