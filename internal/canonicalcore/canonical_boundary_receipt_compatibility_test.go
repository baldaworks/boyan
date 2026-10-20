package canonicalcore_test

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"
	"time"

	core "github.com/baldaworks/boyan/internal/canonicalcore"
	"github.com/baldaworks/boyan/internal/canonicalsql"
	"github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryOldReceiptAfterForgetAndLaterTurn(t *testing.T) {
	runCoreAuthorities(t, func(t *testing.T, factory coreFactory) {
		for _, lost := range []bool{false, true} {
			name := "retained-receipt"
			if lost {
				name = "lost-receipt"
			}

			t.Run(name, func(t *testing.T) { testBoundaryReceiptAfterForget(t, lost, factory) })
		}
	})
}

func testBoundaryReceiptAfterForget(t *testing.T, lost bool, factory coreFactory) {
	t.Helper()
	f := newBoundaryReplayFixture(t, false, factory)
	request := f.request(1, "old-boundary-delivery")
	original, err := f.runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "initial boundary: %v")
	forgetBoundaryAndCommitLaterTurn(t, f)
	f.restart(t)

	if lost {
		deleteBoundaryReceipt(t, f, request)
	}

	before, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "before: %v")
	replay, err := f.runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "old boundary replay: %v")
	after, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "after: %v")

	if before != after || f.model.scenarioCalls != 1 || f.model.profileCalls != 1 ||
		!reflect.DeepEqual(original.OperationIDs, replay.OperationIDs) {
		t.Fatal("old boundary replay repeated effects")
	}

	if !lost {
		original.Disposition = m.IngestDispositionDuplicate
		if !reflect.DeepEqual(original, replay) {
			t.Fatal("old boundary receipt outcome changed")
		}
	}

	request.Fingerprint = "changed-boundary-delivery"
	_, err = f.runtime.ApplySequencedBoundary(context.Background(), request)
	requirePermanentIdentityConflict(t, err)
}

func forgetBoundaryAndCommitLaterTurn(t *testing.T, f *boundaryReplayFixture) {
	t.Helper()

	adapter, err := forgetops.NewCanonicalForgetAdapter(f.store)
	requireNoError(t, err, "forget adapter: %v")
	_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
		SchemaVersion: m.DerivedSchemaVersionV1, Scope: f.export.Scope,
		RequestID: "old-boundary-forget", ForgottenAt: f.at.Add(time.Hour)})
	requireNoError(t, err, "forget: %v")
	processor, err := core.NewCanonicalTurnProcessor(f.store, canonicalExtractorFunc(extractTestStateCandidate),
		m.PolicyRegistry{Version: "policy-v1"})
	requireNoError(t, err, "processor: %v")
	turn, err := m.NewTurn(
		f.export.Scope,
		f.export.Conversation,
		"later-turn",
		f.at.Add(2*time.Hour),
		"Later state",
		"OK",
	)
	requireNoError(t, err, "later turn: %v")
	_, err = processor.ProcessTurn(context.Background(), turn, testProcessorDerivation())
	requireNoError(t, err, "later commit: %v")
}

func deleteBoundaryReceipt(t *testing.T, f *boundaryReplayFixture, request m.SequencedBoundary) {
	t.Helper()

	scope := request.Boundary.Scope

	deleteCoreSQLRecord(
		t,
		f.store,
		coreSQLKey(
			canonicalsql.IngestSequence,
			scope,
			[]byte(request.ProducerID),
			binary.BigEndian.AppendUint64(nil, request.ScopeSequence),
		),
		coreSQLKey(canonicalsql.IngestCursor, scope, []byte(request.ProducerID)),
	)

	return
}
