package canonicalcore_test

import (
	"context"
	"testing"
	"time"

	m "github.com/baldaworks/boyan/pkg/memory"
)

func TestBoundaryOrderedReceiptCannotBypassInputProof(t *testing.T) {
	t.Parallel()
	f := newBoundaryReplayFixture(t, false)
	ctx := context.Background()
	request := f.request(1, "caller-controlled-delivery-fingerprint")
	first, err := f.runtime.ApplySequencedBoundary(ctx, request)
	requireNoError(t, err, "first: %v")
	f.restart(t)
	before, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "before: %v")

	for _, sequence := range []uint64{1, 2} {
		changed := request
		changed.ScopeSequence = sequence
		changed.Boundary.Reason = m.BoundaryReasonReset
		_, err = f.runtime.ApplySequencedBoundary(ctx, changed)
		requirePermanentIdentityConflict(t, err)
	}

	replay, err := f.runtime.ApplySequencedBoundary(ctx, request)
	requireNoError(t, err, "matching receipt: %v")
	after, err := f.store.LoadScopeState(ctx, f.export.Scope)
	requireNoError(t, err, "after: %v")

	if before != after || f.model.scenarioCalls != 1 || f.model.profileCalls != 1 ||
		replay.Disposition != m.IngestDispositionDuplicate || replay.ExportID != first.ExportID {
		t.Fatal("receipt replay changed state, models or original boundary")
	}
}

func TestBoundaryOrderedReceiptRejectsAnotherExportOrKind(t *testing.T) {
	for _, kind := range []m.IngestKind{m.IngestKindTurn, m.IngestKindBoundary} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			f := newBoundaryReplayFixture(t, false)
			request := f.request(1, "supplied-prior-fingerprint")
			state, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
			requireNoError(t, err, "state: %v")

			outcome := m.IngestOutcome{SchemaVersion: m.IngestOutcomeSchemaVersionV1, Kind: kind,
				Disposition: m.IngestDispositionAccepted, Projection: m.ProjectionStatusCurrent,
				ProducerID: request.ProducerID, Scope: f.export.Scope, ScopeSequence: 1,
				Fingerprint: request.Fingerprint, ExportID: "another-export", OperationIDs: []string{"another-operation"},
				ScopeVersion: state.Version, ScopeChangeSeq: state.ChangeSeq}
			err = f.store.SaveIngestSequence(context.Background(), m.IngestSequenceRecord{
				SchemaVersion: m.IngestSequenceSchemaVersionV1, ProducerID: request.ProducerID,
				Scope: f.export.Scope, Sequence: 1, Fingerprint: request.Fingerprint, Outcome: outcome})
			requireNoError(t, err, "seed prior receipt: %v")
			_, err = f.runtime.ApplySequencedBoundary(context.Background(), request)
			requirePermanentIdentityConflict(t, err)

			if f.model.scenarioCalls != 0 || f.model.profileCalls != 0 {
				t.Fatal("receipt collision entered models")
			}
		})
	}
}

func TestBoundaryOrderedPartialRetryAfterRestart(t *testing.T) {
	t.Parallel()
	f := newBoundaryReplayFixture(t, true)
	request := f.request(1, "partial-delivery")

	_, err := f.runtime.ApplySequencedBoundary(context.Background(), request)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeInvalidDerived {
		t.Fatalf("partial error=%v", err)
	}

	f.restart(t)
	before, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "partial state: %v")

	changed := request
	changed.Boundary.OccurredAt = changed.Boundary.OccurredAt.Add(time.Nanosecond)
	_, err = f.runtime.ApplySequencedBoundary(context.Background(), changed)
	requirePermanentIdentityConflict(t, err)
	after, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "changed state: %v")

	if before != after || f.model.scenarioCalls != 1 || f.model.profileCalls != 1 {
		t.Fatal("changed partial retry mutated state/models")
	}

	f.model.invalidProfile = false
	_, err = f.runtime.ApplySequencedBoundary(context.Background(), request)
	requireNoError(t, err, "original partial retry: %v")

	if f.model.scenarioCalls != 1 || f.model.profileCalls != 2 {
		t.Fatal("partial retry did not run profile only")
	}
}

func TestBoundaryOrderedMatchingReceiptRequiresCertificate(t *testing.T) {
	t.Parallel()
	f := newBoundaryReplayFixture(t, false)
	request := f.request(1, "orphan-prior-fingerprint")
	state, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "state: %v")

	outcome := m.IngestOutcome{SchemaVersion: m.IngestOutcomeSchemaVersionV1, Kind: m.IngestKindBoundary,
		Disposition: m.IngestDispositionAccepted, Projection: m.ProjectionStatusCurrent,
		ProducerID: request.ProducerID, Scope: f.export.Scope, ScopeSequence: 1,
		Fingerprint: request.Fingerprint, ExportID: f.export.ExportID,
		OperationIDs: []string{"orphan-scenario", "orphan-profile"},
		ScopeVersion: state.Version, ScopeChangeSeq: state.ChangeSeq}
	err = f.store.SaveIngestSequence(context.Background(), m.IngestSequenceRecord{
		SchemaVersion: m.IngestSequenceSchemaVersionV1, ProducerID: request.ProducerID,
		Scope: f.export.Scope, Sequence: 1, Fingerprint: request.Fingerprint, Outcome: outcome})
	requireNoError(t, err, "seed orphan receipt: %v")
	_, err = f.runtime.ApplySequencedBoundary(context.Background(), request)
	requirePermanentIdentityConflict(t, err)
	after, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "after: %v")

	if state != after || f.model.scenarioCalls != 0 || f.model.profileCalls != 0 {
		t.Fatal("orphan replay changed state/models")
	}
}
