package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}
	operationID, err := scopeForgetOperationID(command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if outcome, found, err := a.loadScopeForgetOutcome(ctx, command.Scope, operationID); err != nil || found {
		if err != nil {
			return memory.ForgetOutcome{}, err
		}
		return outcome, nil
	}
	targets, err := a.loadScopeForgetTargets(ctx, command.Scope)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if err := a.denyScopeForgetTargets(ctx, command.Scope, targets.sourceIDs, targets.revisionIDs, command.ForgottenAt); err != nil {
		return memory.ForgetOutcome{}, err
	}
	outcome, err := a.commitScopeForget(ctx, command, operationID, targets)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Scope, targets.sourceIDs, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}
