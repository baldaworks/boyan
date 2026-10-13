package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) ForgetScope(
	ctx context.Context,
	command memory.ForgetScopeCommand,
) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	operationID, err := scopeForgetOperationID(command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	state, err := a.store.LoadScopeState(ctx, command.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	request := memory.ScopeEraseRequest{
		Scope: command.Scope, OperationID: operationID, ExpectedScopeVersion: state.Version, ForgottenAt: command.ForgottenAt,
		Fingerprint: memory.CanonicalForgetFingerprint(
			operationID,
			command.Scope,
			memory.ForgetKindScope,
			memory.SourceRef{},
			command.RequestID,
		),
	}

	journal, err := a.store.RunScopeErase(ctx, request)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}

	return completedEraseOutcome(journal)
}

func scopeForgetOperationID(command memory.ForgetScopeCommand) (string, error) {
	return memory.CanonicalForgetOperationID(memory.ForgetKindScope, command.Scope, memory.SourceRef{}, command.RequestID)
}

func completedEraseOutcome(journal memory.ScopeEraseJournal) (memory.ForgetOutcome, error) {
	if journal.Stage != memory.ScopeEraseComplete || journal.Outcome == nil {
		return memory.ForgetOutcome{}, memory.RetryableError(memory.CodeConflict, "canonical erase remains incomplete", nil)
	}

	return *journal.Outcome, nil
}
