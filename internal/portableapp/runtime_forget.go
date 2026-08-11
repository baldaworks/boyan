package portableapp

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ForgetSource implements Forgetter.
func (r *Runtime) ForgetSource(ctx context.Context, command memory.ForgetSourceCommand) (memory.ForgetOutcome, error) {
	if r == nil || r.forget == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "boyan forget is unavailable", nil)
	}
	return r.forget.ForgetSource(ctx, command)
}

// ForgetScope implements Forgetter.
func (r *Runtime) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if r == nil || r.forget == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "boyan forget is unavailable", nil)
	}
	return r.forget.ForgetScope(ctx, command)
}
