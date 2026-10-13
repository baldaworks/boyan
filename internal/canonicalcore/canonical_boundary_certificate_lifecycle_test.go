package canonicalcore_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	boundary "github.com/baldaworks/boyan/internal/boundarycore"
	ops "github.com/baldaworks/boyan/internal/canonicalops"
	forget "github.com/baldaworks/boyan/internal/forgetops"
	m "github.com/baldaworks/boyan/pkg/memory"
)

type certificateLifecycleModel struct {
	*inheritanceModel

	failScenario bool
}

func (i *certificateLifecycleModel) SynthesizeScenarios(ctx context.Context,
	request m.ScenarioSynthesisRequest) ([]m.ScenarioCandidate, error) {
	if i.failScenario {
		i.failScenario = false

		return nil, m.RetryableError(m.CodeModelFailure, "fixture scenario failure", nil)
	}

	return i.inheritanceModel.SynthesizeScenarios(ctx, request)
}

func TestBoundaryCertificateBindsOnlySuccessfulStageAndSurvivesForget(t *testing.T) {
	t.Parallel()
	f := newInheritanceFixture(t, false)
	model := &certificateLifecycleModel{inheritanceModel: f.model, failScenario: true}
	adapter, err := ops.NewCanonicalBoundaryStore(f.store, f.reader)
	requireNoError(t, err, "adapter: %v")
	processor, err := boundary.NewBoundaryProcessorWithClock(adapter, model, model,
		testProcessorDerivation(), func() time.Time { return f.now })
	requireNoError(t, err, "processor: %v")

	_, err = processor.ProcessBoundary(context.Background(), f.export)
	if code, _, ok := m.ClassifyError(err); !ok || code != m.CodeModelFailure {
		t.Fatalf("scenario fixture should fail: %v", err)
	}

	identity, err := f.store.VerifyCanonicalBoundary(context.Background(), f.export)
	requireNoError(t, err, "uncommitted identity: %v")

	if identity.Certified {
		t.Fatal("failed model output reserved boundary identity")
	}

	committed, err := processor.ProcessBoundary(context.Background(), f.export)
	requireNoError(t, err, "successful retry: %v")
	identity, err = f.store.VerifyCanonicalBoundary(context.Background(), f.export)
	requireNoError(t, err, "committed identity: %v")

	if !identity.Certified {
		t.Fatal("successful stage did not atomically bind input")
	}

	assertCertifiedBoundaryForgetReplay(t, f, processor, committed)
}

func assertCertifiedBoundaryForgetReplay(t *testing.T, f *inheritanceFixture,
	processor *boundary.BoundaryProcessor, committed m.BoundaryOutcome) {
	t.Helper()
	payloads := boundaryCertificatePayloads(t, f, committed)
	adapter, err := forget.NewCanonicalForgetAdapter(f.store)
	requireNoError(t, err, "forget adapter: %v")
	_, err = adapter.ForgetScope(context.Background(), m.ForgetScopeCommand{
		SchemaVersion: m.DerivedSchemaVersionV1, Scope: f.export.Scope,
		RequestID: "forget-certified-boundary", ForgottenAt: f.now.Add(time.Hour),
	})
	requireNoError(t, err, "forget: %v")
	identity, err := f.store.VerifyCanonicalBoundary(context.Background(), f.export)
	requireNoError(t, err, "identity after forget: %v")

	if !identity.Certified {
		t.Fatal("forget removed immutable content-free replay proof")
	}

	before, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "state before forgotten replay: %v")

	scenarioCalls, profileCalls := f.model.scenarioCalls, f.model.profileCalls
	replayed, err := processor.ProcessBoundary(context.Background(), f.export)
	requireNoError(t, err, "matching forgotten replay: %v")
	after, err := f.store.LoadScopeState(context.Background(), f.export.Scope)
	requireNoError(t, err, "state after forgotten replay: %v")

	if !reflect.DeepEqual(replayed, committed) || before != after ||
		f.model.scenarioCalls != scenarioCalls || f.model.profileCalls != profileCalls {
		t.Fatal("forgotten replay changed original outcomes, state or model calls")
	}

	assertForgottenBoundaryPayloads(t, f, payloads)
}

func boundaryCertificatePayloads(t *testing.T, f *inheritanceFixture,
	committed m.BoundaryOutcome) []m.PayloadRef {
	t.Helper()

	var payloads []m.PayloadRef

	for _, stage := range []m.OperationOutcome{committed.Scenarios, committed.Profile} {
		operation, found, err := f.store.LoadCanonicalOperation(context.Background(), f.export.Scope, stage.OperationID)
		requireNoError(t, err, "operation: %v")

		if !found {
			t.Fatal("committed stage operation missing")
		}

		revisions, err := f.store.LoadCanonicalRevisions(context.Background(), m.CanonicalRevisionReadRequest{
			Scope: f.export.Scope, RevisionIDs: operation.Outcome.RevisionIDs,
		})
		requireNoError(t, err, "stage metadata: %v")

		for _, revision := range revisions {
			payloads = append(payloads, revision.Payload)
		}
	}

	return payloads
}

func assertForgottenBoundaryPayloads(t *testing.T, f *inheritanceFixture, payloads []m.PayloadRef) {
	t.Helper()

	heads, err := f.store.ScanActiveHeads(context.Background(), m.ActiveHeadScanRequest{Scope: f.export.Scope, Limit: 10})
	requireNoError(t, err, "heads after forgotten replay: %v")

	if len(heads) != 0 {
		t.Fatal("forgotten replay recreated active revisions")
	}

	for _, payload := range payloads {
		if _, err := f.store.LoadPayload(context.Background(), payload); err == nil {
			t.Fatal("forgotten replay recreated erased payload")
		}
	}
}
