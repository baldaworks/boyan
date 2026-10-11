package orderedingest //nolint:testpackage // Exercises shared private test fixtures within the package.

import (
	"context"

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
