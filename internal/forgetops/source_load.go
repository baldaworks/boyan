package forgetops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

func (a *CanonicalForgetAdapter) loadSourceForgetID(ctx context.Context, command memory.ForgetSourceCommand) (string, error) {
	return a.store.CanonicalSourceID(ctx, command.Source.Scope, command.Source)
}

func (a *CanonicalForgetAdapter) loadSourceForgetOutcome(ctx context.Context, scope memory.Scope, operationID string) (memory.ForgetOutcome, bool, error) {
	return a.store.LoadCanonicalForgetOutcome(ctx, scope, operationID, memory.ForgetKindSource)
}
