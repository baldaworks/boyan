package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func scopeForgetOperationID(command memory.ForgetScopeCommand) (string, error) {
	return memory.CanonicalForgetOperationID(memory.ForgetKindScope, command.Scope, memory.SourceRef{}, command.RequestID)
}

func (a *CanonicalForgetAdapter) loadScopeForgetOutcome(ctx context.Context, scope memory.Scope, operationID string) (memory.ForgetOutcome, bool, error) {
	return a.store.LoadCanonicalForgetOutcome(ctx, scope, operationID, memory.ForgetKindScope)
}

func (a *CanonicalForgetAdapter) commitScopeForget(
	ctx context.Context,
	command memory.ForgetScopeCommand,
	operationID string,
	targets scopeForgetTargets,
) (memory.ForgetOutcome, error) {
	return a.store.CommitCanonicalForget(ctx, memory.CanonicalForgetCommitRequest{
		Scope:                command.Scope,
		OperationID:          operationID,
		Kind:                 memory.ForgetKindScope,
		Fingerprint:          memory.CanonicalForgetFingerprint(operationID, command.Scope, memory.ForgetKindScope, memory.SourceRef{}, command.RequestID),
		ExpectedScopeVersion: targets.scopeVersion,
		Sources:              targets.sources,
		Revisions:            targets.revisions,
		ForgottenAt:          command.ForgottenAt,
	})
}
