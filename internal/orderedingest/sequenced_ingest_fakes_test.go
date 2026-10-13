package orderedingest //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"
	"testing"

	boyan "github.com/baldaworks/boyan/pkg/memory"
)

type fakeTurnProcessor struct {
	outcome boyan.CanonicalMutationOutcome
	err     error
	process func(context.Context, boyan.Turn, boyan.DerivationRef) (boyan.CanonicalMutationOutcome, error)
}

func (f fakeTurnProcessor) ProcessTurn(
	ctx context.Context,
	turn boyan.Turn,
	derivation boyan.DerivationRef,
) (
	boyan.CanonicalMutationOutcome,
	error,
) {
	if f.process != nil {
		return f.process(ctx, turn, derivation)
	}

	return f.outcome, f.err
}

type fakeBoundaryProcessor struct {
	outcome boyan.BoundaryOutcome
	err     error
}

func (f fakeBoundaryProcessor) ProcessBoundary(context.Context, boyan.Boundary) (boyan.BoundaryOutcome, error) {
	return f.outcome, f.err
}

type fakeCanonicalStore struct {
	state boyan.ScopeState
	err   error
}

func (s fakeCanonicalStore) LoadScopeState(_ context.Context, scope boyan.Scope) (boyan.ScopeState, error) {
	if s.err != nil {
		return boyan.ScopeState{}, s.err
	}

	if s.state != (boyan.ScopeState{}) {
		return s.state, nil
	}

	return boyan.ScopeState{}, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) ApplyCanonicalMutation(
	context.Context,
	boyan.CanonicalMutation,
) (
	boyan.CanonicalMutationOutcome,
	error,
) {
	return boyan.CanonicalMutationOutcome{}, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) ScanScopeChanges(context.Context, boyan.Scope, uint64, uint32) ([]boyan.ScopeChange, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) LoadCanonicalRevisions(
	context.Context,
	boyan.CanonicalRevisionReadRequest,
) (
	[]boyan.MemoryRevision,
	error,
) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) ScanActiveHeads(context.Context, boyan.ActiveHeadScanRequest) ([]boyan.ItemHead, error) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) ScanActiveMemory(
	context.Context,
	boyan.ActiveMemoryScanRequest,
) (
	[]boyan.ActiveCanonicalMemory,
	error,
) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) ClaimDeliveryOutbox(
	context.Context,
	boyan.DeliveryClaimRequest,
) (
	[]boyan.ClaimedDelivery,
	error,
) {
	return nil, boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

func (fakeCanonicalStore) SettleDeliveryOutbox(context.Context, boyan.DeliverySettlementRequest) error {
	return boyan.PermanentError(boyan.CodeDisabled, "unused in turn test", nil)
}

// identityTurnProcessor models committed authoritative input separately from a
// producer's delivery fingerprint, so receipt tests exercise content checking.
type identityTurnProcessor struct {
	fakeTurnProcessor

	fingerprint string
}

func (f identityTurnProcessor) VerifyCanonicalTurn(
	_ context.Context,
	turn boyan.Turn,
) (boyan.CanonicalTurnIdentityResult, error) {
	actual, err := boyan.CanonicalTurnFingerprint(turn)
	if err != nil {
		return boyan.CanonicalTurnIdentityResult{}, err
	}

	if actual != f.fingerprint {
		return boyan.CanonicalTurnIdentityResult{}, boyan.PermanentError(
			boyan.CodeConflict,
			"authoritative test turn differs",
			nil,
		)
	}

	normalized, err := boyan.NormalizeCanonicalTurn(turn)
	if err != nil {
		return boyan.CanonicalTurnIdentityResult{}, err
	}

	source := boyan.CanonicalSourceRecord{SourceID: normalized.SourceID, Scope: normalized.Scope,
		Sensitivity: boyan.SensitivityStandard, Retention: boyan.RetentionClassStandard,
		Payload: boyan.PayloadRef{ID: "source-payload", Digest: "source-digest", ByteSize: 1},
	}

	proof := boyan.CanonicalTurnIdentityResult{Found: true, Source: source}
	for _, message := range normalized.Messages {
		proof.Messages = append(proof.Messages, boyan.MessageRecord{MessageID: message.MessageID,
			SourceID: normalized.SourceID,
			Role:     message.Role, Payload: boyan.PayloadRef{ID: "message-payload", Digest: "message-digest", ByteSize: 1}})
	}

	return proof, nil
}

func newIdentityTurnProcessor(
	t *testing.T,
	turn boyan.Turn,
	outcome boyan.CanonicalMutationOutcome,
) identityTurnProcessor {
	t.Helper()

	fingerprint, err := boyan.CanonicalTurnFingerprint(turn)
	requireNoError(t, err, "committed turn fingerprint: %v")

	return identityTurnProcessor{fakeTurnProcessor: fakeTurnProcessor{outcome: outcome}, fingerprint: fingerprint}
}
