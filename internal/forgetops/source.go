package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) ForgetSource(
	ctx context.Context,
	command memory.ForgetSourceCommand,
) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	sourceID, err := a.store.CanonicalSourceID(ctx, command.Source.Scope, command.Source)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	operationID, err := sourceForgetOperationID(command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	state, err := a.store.LoadScopeState(ctx, command.Source.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	request := memory.ScopeEraseRequest{
		Kind: memory.ForgetKindSource, SourceID: sourceID, SourceRef: command.Source, Scope: command.Source.Scope,
		OperationID: operationID, ExpectedScopeVersion: state.Version, ForgottenAt: command.ForgottenAt,
		Fingerprint: memory.CanonicalForgetFingerprint(
			operationID,
			command.Source.Scope,
			memory.ForgetKindSource,
			command.Source,
			"",
		),
	}

	journal, err := a.store.RunScopeErase(ctx, request)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	return completedEraseOutcome(journal)
}
