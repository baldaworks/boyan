package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error) {
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}
	sourceID, err := a.loadSourceForgetID(ctx, command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	operationID, err := sourceForgetOperationID(command)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if outcome, found, err := a.loadSourceForgetOutcome(ctx, command.Source.Scope, operationID); err != nil || found {
		if err != nil {
			return memory.ForgetOutcome{}, err
		}
		if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{sourceID}, outcome.Revisions); scrubErr != nil {
			return outcome, scrubErr
		}
		return outcome, nil
	}
	targets, err := a.loadSourceForgetTargets(ctx, command, sourceID)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	outcome, err := a.commitSourceForget(ctx, command, operationID, targets)
	if err != nil {
		return memory.ForgetOutcome{}, err
	}
	if scrubErr := a.scrubForgetOutcome(ctx, command.Source.Scope, []string{targets.sourceID}, outcome.Revisions); scrubErr != nil {
		return outcome, scrubErr
	}
	return outcome, nil
}
