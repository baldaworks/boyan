//go:build integration

package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestNativePayloadForgetPreservesOtherScopeKind(t *testing.T) {
	t.Parallel()
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		reader, processor, turn, _, store := nativeFlow(t, factory)
		ctx := context.Background()
		other := turn
		other.Scope.Kind = m.ScopeKindGroup
		outcome, err := processor.ProcessTurn(ctx, other, testProcessorDerivation())
		requireNoError(t, err, "other scope process: %v")
		records, err := reader.LoadRecallRecords(ctx, other.Scope, outcome.RevisionIDs)
		requireNoError(t, err, "other scope records: %v")

		if len(records) != 1 {
			t.Fatalf("other scope records: %d", len(records))
		}

		request := m.TraceRequest{Scope: other.Scope,
			Root: m.RevisionRef{ItemID: records[0].ItemID, RevisionID: records[0].RevisionID}, MaxNodes: 10,
		}
		_, err = reader.Trace(ctx, request)
		requireNoError(t, err, "trace before forgetting: %v")
		adapter, err := forgetops.NewCanonicalForgetAdapter(store)
		requireNoError(t, err, "forget adapter: %v")
		_, err = adapter.ForgetScope(ctx, m.ForgetScopeCommand{
			SchemaVersion: m.DerivedSchemaVersionV1, Scope: turn.Scope,
			RequestID: "isolated-forget", ForgottenAt: time.Now().UTC(),
		})
		requireNoError(t, err, "forget personal scope: %v")
		traced, err := reader.Trace(ctx, request)
		requireNoError(t, err, "other scope trace after forgetting: %v")

		if len(traced.Sources) != 1 || traced.Sources[0].Turn.Messages[0].Text != turn.Messages[0].Text {
			t.Fatalf("other scope source lost: %+v", traced.Sources)
		}
	})
}
