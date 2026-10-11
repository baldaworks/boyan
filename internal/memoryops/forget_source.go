package memoryops

import (
	"context"

	memory "github.com/baldaworks/boyan/pkg/memory"
)

// ForgetSource delegates a source denial without widening its scope.
func (s *ForgetService) ForgetSource(
	ctx context.Context,
	command memory.ForgetSourceCommand,
) (
	memory.ForgetOutcome,
	error,
) {
	if s == nil || s.operations == nil {
		return memory.ForgetOutcome{}, memory.PermanentError(memory.CodeDisabled, "forget service is unavailable", nil)
	}

	if err := command.Validate(); err != nil {
		return memory.ForgetOutcome{}, err
	}

	outcome, err := s.operations.ForgetSource(ctx, command)

	return s.syncProjection(ctx, command.Source.Scope, outcome, err)
}
