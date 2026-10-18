package canonicalsql

import (
	"context"
	"github.com/baldaworks/boyan/internal/forgetops"
	memory "github.com/baldaworks/boyan/pkg/memory"
	"testing"
	"time"
)

func TestEraseActualForgetInvalidatesCapturedReaders(t *testing.T) {
	for _, kind := range []string{"source", "scope", "begin", "deny-page"} {
		t.Run(kind, func(t *testing.T) {
			for _, stage := range []string{"trace-preflight", "trace-hydrated", "recall-hydrated", "boundary-hydrated"} {
				t.Run(stage, func(t *testing.T) {
					forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
						runEraseReaderInterleaving(t, o, reopen, kind, stage)
					})
				})
			}
		})
	}
}

func erasePrivacyRequest(h privacyHistory) memory.ScopeEraseRequest {
	return memory.ScopeEraseRequest{Scope: h.scope, OperationID: "erase", Fingerprint: "fp",
		ExpectedScopeVersion: 1, ForgottenAt: h.turn.CompletedAt}
}
func runActualForget(ctx context.Context, o *Owner, h privacyHistory, kind string) error {
	adapter, err := forgetops.NewCanonicalForgetAdapter(o)
	if err != nil {
		return err
	}

	switch kind {
	case "source":
		ref, _, _ := canonicalSourceRef(h.payloads[0].Data, h.scope, h.source.SourceID)
		_, err = adapter.ForgetSource(ctx, memory.ForgetSourceCommand{SchemaVersion: memory.DerivedSchemaVersionV1,
			Source: ref, ForgottenAt: h.turn.CompletedAt})
	case "scope":
		_, err = adapter.ForgetScope(ctx, memory.ForgetScopeCommand{SchemaVersion: memory.DerivedSchemaVersionV1,
			Scope: h.scope, RequestID: "forget", ForgottenAt: h.turn.CompletedAt})
	default:
		var j memory.ScopeEraseJournal

		j, err = o.BeginScopeErase(ctx, erasePrivacyRequest(h))
		for err == nil && kind == "deny-page" && j.Stage != memory.ScopeEraseDenyRevisions {
			j, err = o.AdvanceScopeErase(ctx, h.scope, "erase")
		}
	}

	return err
}

func TestEraseActualWriterFencesConcurrentProofMutation(t *testing.T) {
	forEachOwner(t, func(t *testing.T, o *Owner, reopen func() *Owner) {
		h := prepareTraceHistory(t, o)
		r := erasePrivacyRequest(h)
		_, err := o.BeginScopeErase(t.Context(), r)
		requireEraseTestNoError(t, err)

		mutation := historyMutation(h)
		mutation.Operation.OperationID = "new-proof"
		mutation.ExpectedScopeVersion = 1
		mutation.TurnIdentity = proofTurnMutation(t, h).TurnIdentity
		start := make(chan struct{})
		writeDone := make(chan error, 1)
		eraseDone := make(chan error, 1)

		go func() { <-start; _, err := o.ApplyCanonicalMutation(t.Context(), mutation); writeDone <- err }()
		go func() { <-start; _, err := o.RunScopeErase(t.Context(), r); eraseDone <- err }()

		close(start)

		if err := <-writeDone; err == nil {
			t.Fatal("proof mutation crossed actual erase boundary")
		}

		requireEraseTestNoError(t, <-eraseDone)
		requireEraseTestNoError(t, o.Close())
		o = reopen()

		_, found, err := o.LoadCanonicalOperation(t.Context(), h.scope, "new-proof")
		if err != nil || found {
			t.Fatalf("cross-boundary operation=%t %v", found, err)
		}

		mutation.ExpectedScopeVersion = 2
		if _, err := o.ApplyCanonicalMutation(t.Context(), mutation); err == nil {
			t.Fatal("copied proof resurrected source")
		}

		for _, p := range h.payloads {
			if _, err := o.LoadPayload(t.Context(), p.Ref); err == nil {
				t.Fatal("proof retry resurrected erased payload")
			}
		}
	})
}

func runEraseReaderInterleaving(t *testing.T, o *Owner, reopen func() *Owner, kind, stage string) {
	t.Helper()
	h := prepareTraceHistory(t, o)
	request := memory.TraceRequest{Scope: h.scope, Root: memory.RevisionRef{ItemID: h.item.ItemID,
		RevisionID: h.revision.RevisionID}, MaxNodes: 10}
	graph, err := o.preflightTraceGraph(t.Context(), request)
	requireEraseTestNoError(t, err)

	captured := captureEraseReader(t, o, h, request, &graph, stage)
	// The public adapter/erase call executes on another goroutine between
	// actual native read snapshots, with no callback under owner admission.
	done := make(chan error, 1)
	go func() { done <- runActualForget(t.Context(), o, h, kind) }()

	requireEraseTestNoError(t, <-done)

	switch stage {
	case "trace-preflight":
		err = o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			return hydrateTraceGraph(ctx, tx,
				request, &graph)
		})
	case "trace-hydrated":
		err = o.finalTracePrivacyBarrier(t.Context(), graph)
	default:
		err = o.finalReaderBarrier(t.Context(), captured)
	}

	if err == nil {
		t.Fatal("public erase allowed stale hydrated content")
	}

	if kind == "begin" || kind == "deny-page" {
		j, err := o.RunScopeErase(t.Context(), erasePrivacyRequest(h))
		requireEraseTestNoError(t, err)

		if j.Stage != memory.ScopeEraseComplete {
			t.Fatal("pending erase not completed")
		}
	}

	requireEraseTestNoError(t, o.Close())
	o = reopen()

	assertErasedReaderReopen(t, o, h, request)
}

func captureEraseReader(t *testing.T, o *Owner, h privacyHistory, request memory.TraceRequest,
	graph *traceGraph, stage string) readerSnapshot {
	t.Helper()

	var captured readerSnapshot

	if stage == "trace-hydrated" {
		requireEraseTestNoError(t, o.View(t.Context(), func(ctx context.Context,
			tx *Tx) error {
			return hydrateTraceGraph(ctx, tx, request, graph)
		}))
	}

	if stage == "recall-hydrated" {
		requireEraseTestNoError(t, o.View(t.Context(), func(ctx context.Context, tx *Tx) error {
			records, err := loadReaderRecords(ctx, tx, h.scope, []string{h.revision.RevisionID}, time.Now(), &captured)
			if err == nil && len(records) != 1 {
				t.Fatal("fixture did not hydrate")
			}

			return err
		}))
	}

	if stage == "boundary-hydrated" {
		records, snapshot, err := o.loadBoundedBoundaryRecords(t.Context(), h.scope, memory.MaxSnapshotItems, time.Now())
		requireEraseTestNoError(t, err)

		if len(records) != 1 {
			t.Fatal("fixture did not collect boundary")
		}

		captured = snapshot
	}

	return captured
}
func assertErasedReaderReopen(t *testing.T, o *Owner, h privacyHistory, request memory.TraceRequest) {
	t.Helper()

	denied, err := o.IsSourceDenied(t.Context(), h.scope, h.source.SourceID)
	if err != nil || !denied {
		t.Fatalf("reopen denial=%t %v", denied, err)
	}

	response, err := o.Trace(t.Context(), request)
	if err == nil || len(response.Sources) != 0 {
		t.Fatalf("reopened raw trace=%+v %v", response, err)
	}

	view, err := o.LoadCanonicalBoundaryView(t.Context(), h.scope)
	if err != nil || len(view.Snapshot.Atoms) != 0 {
		t.Fatalf("reopened boundary=%+v %v", view, err)
	}
}
