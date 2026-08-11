package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) commitSourceForget(
	ctx context.Context,
	command memory.ForgetSourceCommand,
	operationID string,
	targets sourceForgetTargets,
) (memory.ForgetOutcome, error) {
	return a.store.CommitCanonicalForget(ctx, memory.CanonicalForgetCommitRequest{
		Scope:                command.Source.Scope,
		OperationID:          operationID,
		Kind:                 memory.ForgetKindSource,
		Fingerprint:          memory.CanonicalForgetFingerprint(operationID, command.Source.Scope, memory.ForgetKindSource, command.Source, ""),
		ExpectedScopeVersion: targets.scopeVersion,
		Sources:              []memory.SourceRef{command.Source},
		Revisions:            targets.revisions,
		ForgottenAt:          command.ForgottenAt,
	})
}
