package memoryops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ForgetScope delegates an exact-scope denial without exposing a remote
// mutation surface.
func (s *ForgetService) ForgetScope(ctx context.Context, command memory.ForgetScopeCommand) (memory.ForgetOutcome, error) {
	if s == nil || s.operations == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "forget service is unavailable", nil)
	}
	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}
	outcome, err := s.operations.ForgetScope(ctx, command)
	return s.syncProjection(ctx, command.Scope, outcome, err)
}
